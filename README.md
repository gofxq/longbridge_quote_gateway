# Longbridge Quote Gateway

只读的 Longbridge 行情 REST 网关，使用 Go、Gin、官方 Go SDK 和内存缓存。提供 PRD 第一阶段列出的全部 11 个 HTTP 路由，不创建交易上下文。单账户只运行一个实例。

## 启动

需要 Docker Compose，或 Go 1.26.8。复制环境模板，生成网关 Token，填写 Longbridge 凭证：

```sh
cp .env.example .env
openssl rand -hex 32
# 编辑 .env，将生成值写入 GATEWAY_TOKEN，并填写三个 LONGBRIDGE 凭证
chmod 600 .env
docker compose up -d --build
curl http://127.0.0.1:12280/healthz
curl http://127.0.0.1:12280/readyz
```

Compose 显式设置容器内 `HTTP_ADDR=:12280`（覆盖 `.env` 的监听地址），仅发布到本机 12280，容器使用非 root 用户、只读根文件系统、256 MiB 内存上限和 200 MiB Go 软内存目标。根据实际负载调整；64 MiB 缓存预算不等于整个进程占用。不要启动多个副本。

本地运行需将 `.env` 中配置导出为环境变量（程序不会自动加载 `.env`）；HTTP_ADDR 不设置时绑定 `127.0.0.1:12280`。`make build`、`make check` 分别构建和执行静态检查、race 测试。

```sh
curl -H "Authorization: Bearer $GATEWAY_TOKEN" \
  'http://127.0.0.1:12280/api/v1/quotes?symbols=AAPL.US,TSLA.US'
curl -H "Authorization: Bearer $GATEWAY_TOKEN" \
  'http://127.0.0.1:12280/api/v1/candlesticks?symbol=AAPL.US&period=1d&count=100&adjust=none'
```

浏览器打开 `/docs` 查看 Swagger UI（静态资源来自固定版本 CDN），点击 Authorize 输入网关 Token。离线客户端可使用仓库的 `internal/server/openapi.json`，服务的 `/openapi.json` 与 `/metrics` 均需要 Bearer Token。

## API

| 路径（GET /api/v1）     | 参数                                           |
| ----------------------- | ---------------------------------------------- |
| `/quotes`               | `symbols`，逗号分隔                            |
| `/quotes/{symbol}`      | 路径标的                                       |
| `/securities`           | `symbols`                                      |
| `/candlesticks`         | `symbol, period=1d, count=100, adjust=none`    |
| `/candlesticks/history` | `symbol, start, end, period=1d, adjust=none`   |
| `/intraday`             | `symbol`                                       |
| `/depth`                | `symbol`                                       |
| `/trades`               | `symbol, count=100`                            |
| `/calc-indexes`         | `symbols, indexes`，均逗号分隔                 |
| `/trading-sessions`     | 无                                             |
| `/trading-days`         | `market=US/HK/CN, start, end`，区间最多 365 天 |

标的支持 US、HK、SH、SZ 后缀，去空格、转大写、按首次出现顺序去重；批量数量在去重前检查。支持周期 `1m,5m,15m,30m,1d,1w,1mo,1y`，复权为 `none/forward`，数量最大 1000。日期为 `YYYY-MM-DD`，历史和普通 K 线均固定盘中时段。未知参数、重复参数和 URL Token 均拒绝。

计算指标支持 `last_done, change_val, change_rate, volume, turnover, turnover_rate, total_market_value, pe_ttm_ratio, pb_ratio, dividend_ratio_ttm`。指标集合排序去重后参与缓存 Key。SDK 未提供的值保留 null；不把缺值补成行情。

业务响应固定包含 `code,message,data,errors,request_id,meta`。批量接口全部成功为 `OK`，部分成功为 HTTP 200 `PARTIAL_SUCCESS`，全部失败为 HTTP 502 `BATCH_FAILED`。请检查 `errors`；成功数据和错误列表分别保持请求顺序。单只报价的 `data` 为对象，批量为数组。

价格和金额为字符串，时间戳为 UTC Unix 秒。报价涨跌使用 decimal：金额保留输入最大精度，百分比保留两位小数、半值远离零；缺值和非正昨收不会触发除零。盘前、盘后、夜盘数据独立。

`meta.symbols` 保存逐标的缓存来源与原始写入时间；交易时段及交易日历使用 `meta.cache`。超大项仍返回成功，但 `stored=false`。历史 K 线升序返回，`meta` 含请求日期、实际首尾时间、根数和 `possibly_truncated`，达到 1000 根保守标记截断，不自动分页。缓存命中保留这些信息。

## 架构与边界

```text
HTTP/Gin → 参数校验 → Service（按 Key 合并、短窗口批处理）
                         ↓              ↓
                  有界 TTL/LRU 缓存   有界队列 → 平滑限速 → 固定 worker → Provider → SDK
```

- `internal/model`：独立于 SDK 的 JSON 合约和业务查询。
- `internal/server`：鉴权、入口保护、校验、响应组装和 OpenAPI。
- `internal/service`：有界 singleflight 注册表；登记、缓存检查与发布互斥；固定 worker，无逐请求后台 goroutine。
- `internal/cache`：不可变序列化数据，TTL、条目/字节/单项预算、LRU 和过期清理。
- `internal/provider/longbridge`：SDK 生命周期、显式数据转换、按错误类型和端点映射。

共享任务从应用 Context 派生，首次登记后总预算 8 秒、调度预算 1 秒，后续等待者不延长。客户端取消只退出自己的等待，任务可以继续填充缓存。HTTP 请求默认 10 秒、128 在途；上游默认 4 并发、8 RPS、32 个等待批次、512 个唯一 Key。后台检查使排队超时不受运行中的 SDK 调用阻塞。过期批次在 worker 取出时回收队列槽位；它们在此之前仍保守占用有界队列容量。

缓存默认 60 秒，20,000 条、64 MiB Key 加负载、单负载 2 MiB。命中不续期，不返回过期数据，不缓存错误，不支持强制刷新。条目读取复制数据，避免调用方修改共享负载。

入口同时按单个 Token 总流量和连接来源 IP 限流，默认 30 QPS、突发 60。IP 表上限 4096，满后清除空闲超过 5 分钟的记录，否则拒绝新增来源。默认不信任任何代理头；反向代理后的客户端共享代理 IP 的限额。

`/healthz` 仅判断 HTTP 存活；`/readyz` 使用初始化、最近请求结果和 SDK 重连回调，不主动查询行情。SDK 未暴露实时断线状态，因此静默断线与状态反映之间可能有延迟。初始化失败按 1–30 秒指数退避；连接建立后由 SDK 自动重连。SIGTERM/SIGINT 取消任务、唤醒等待者、关闭 HTTP 与 SDK。

日志使用 JSON，记录请求 ID、路由、状态及耗时；debug 模式另含标的和命中数量。不记录查询串、Authorization 或上游原始错误参数。Prometheus 指标接口受 Token 保护。

## OAuth

传统 API Key 是默认方式。OAuth 使用 SDK 自动刷新和持久化：

1. 在操作员机器设置 `LONGBRIDGE_OAUTH_CLIENT_ID`，运行 `go run ./cmd/auth`，打开输出的 URL 完成授权。
2. SDK 将凭证保存到 `~/.longbridge/openapi/tokens/<client_id>`。将该目录安全挂载到容器 `/home/nonroot/.longbridge/openapi/tokens`，需允许 UID 65532 写入，以便刷新后保存。
3. 设置服务的 `LONGBRIDGE_OAUTH_CLIENT_ID`；服务要求已有 Token 文件，不在日志输出授权 URL。过期且无法刷新时初始化失败，应重新运行授权命令。

不要将 OAuth 文件或 `.env` 提交到仓库。不要修改 HOME 指向其他用户目录。

## 部署与验证

Caddy 同主机示例：

```caddy
quote.h.example.com {
    encode zstd gzip
    reverse_proxy 127.0.0.1:12280
}
```

跨容器代理使用共享 Docker 网络和服务名。远程访问应由 HTTPS 代理终止 TLS。

`make check` 覆盖并发交叉合并、取消隔离、缓存容量/过期/不可变、队列/总预算、错误不缓存、HTTP 信封和部分失败、历史截断、decimal 舍入及 SDK 转换回归。真实账户权限、市场数据、外网重连和 256 MiB 长时负载需部署后验证，自动化测试不使用真实凭证。

依赖固定在 go.mod/go.sum。官方 SDK v0.30.1 的换手率转换错误通过 `third_party/openapi-go` 的单行补丁修复；来源、许可证与移除条件见 `third_party/README.md`。锁定版本 WebSocket 路径没有内置限速器，统一频控由网关承担；另测试了 SDK HTTP 重试对 Context 的响应。

参考：[官方 Go SDK](https://pkg.go.dev/github.com/longbridge/openapi-go/quote)、[历史 K 线限制](https://open.longbridge.com/docs/quote/pull/history-candlestick)、[分时错误码](https://open.longbridge.com/docs/quote/pull/intraday)。

## 真实配置集成测试

先使用 `.env` 启动一个网关实例，再在仓库根目录运行：

```sh
python3 scripts/live_test.py
python3 scripts/live_boundaries.py
```

脚本仅查询行情，读取 `.env` 中的网关 Token，不输出凭证或行情原文。默认访问 `http://127.0.0.1:12280`，可通过 `GATEWAY_TEST_URL` 指定自己的网关地址。基础测试包括真实缓存 TTL 等待，约需 1–2 分钟；额外测试覆盖全部 K 线周期、前复权、1000 根 K 线和历史截断。K 线测试使用 AAPL.US，会计入账户的该标的查询配额。

仅在测试期间没有其他客户端流量时，上游调用次数断言才有意义。测试使用默认 30 QPS/突发 60 的入口配置；修改限流参数或 TTL 后需同步调整预期。运行结果保存为忽略提交的 `live-test-results.json` 和 `live-test-extended.json`。最近一次完整实测及额外断网/重启测试记录见 `docs/live-test-report.json`，这是一次运行快照，不代表持续运行保证。
