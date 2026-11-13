# **Longbridge 行情 API 网关服务设计方案**

**项目名称：Longbridge Quote Gateway**

**目标：** 将 [Longbridge OpenAPI](https://open.longbridge.com/zh-CN/docs) 提供的行情能力封装成一个可通过 Docker 部署的 HTTP REST 服务，客户端只需要提供一个简单的 Token，即可查询美股、港股、A 股及相关金融产品的行情。

## **一、总体设计**

### **1.1 推荐技术方案**

建议采用 **Go + Gin + Longbridge 官方 Go SDK + 内存缓存**。

| 模块       | 技术选型                           | 设计原因                             |
| ---------- | ---------------------------------- | ------------------------------------ |
| 开发语言   | Go                                 | 资源占用低、并发处理方便、适合容器化 |
| HTTP 框架  | Gin                                | 简单、成熟，适合 REST API            |
| 行情客户端 | `github.com/longbridge/openapi-go` | 官方 SDK，无需自行实现行情通信协议   |
| 客户端鉴权 | Bearer Token                       | 静态 Token，使用门槛低               |
| 缓存       | 内存 TTL Cache                     | 缓存 60 秒，无需额外部署 Redis       |
| 并发控制   | `singleflight` + Semaphore         | 合并重复请求，限制上游并发           |
| 接口文档   | OpenAPI 3.0 / Swagger              | 方便其他服务、脚本和 AI Agent 调用   |
| 配置       | 环境变量                           | 与 Docker Compose 集成               |
| 部署       | Docker Compose                     | 单容器即可运行                       |
| 日志       | JSON 结构化日志                    | 方便排查问题和接入监控               |

初期不引入数据库、Redis、消息队列，也不需要部署前端管理页面。

### **1.2 系统架构**

```mermaid
flowchart LR
    client["客户端<br/>Python / Go / curl / AI Agent"] --> api["HTTP REST API<br/>Gin"]
    api --> auth["Bearer Token 鉴权"]
    auth --> validate["请求参数校验"]
    validate --> cache{"60 秒缓存"}
    cache -->|命中| response["返回响应"]
    cache -->|未命中| singleflight["请求合并<br/>singleflight"]
    singleflight --> concurrency["并发控制"]
    concurrency --> sdk["Longbridge QuoteContext<br/>官方 Go SDK"]
    sdk --> upstream["Longbridge 行情服务"]
    upstream --> write["写入内存缓存"]
    write --> response
```

请求处理流程：

1. 客户端携带 `Authorization: Bearer <token>` 发起请求。
2. 服务验证 Token，校验股票代码、周期、数量等参数。
3. 按请求参数生成缓存 Key，查询内存缓存。
4. 缓存命中时直接返回，不调用 Longbridge。
5. 缓存未命中时合并相同的并发请求，并在共享任务内部再次检查缓存。
6. 通过有界队列、并发控制和限速器调度任务，再使用官方 SDK 查询行情。
7. 在缓存容量允许时将成功获取的数据缓存 60 秒，并按统一结构返回；批量部分失败的规则见 2.7。

### **1.3 Longbridge 的实际限制**

根据官方文档，设计时需要考虑以下限制：

| 项目         | 官方限制或特性                     | 设计处理                       |
| ------------ | ---------------------------------- | ------------------------------ |
| 调用频率     | 每秒最多 10 次                     | SDK 自身限速，网关增加请求控制 |
| 上游并发     | 最多 5 个请求                      | 网关并发限制不超过 5           |
| 长连接       | 每个账户同时只能建立 1 个          | 单实例、单 `QuoteContext`      |
| 批量报价     | 单次最多 500 个标的                | 网关默认限制 50 个             |
| 行情推送订阅 | 最多 500 个标的                    | 第一版暂不开放推送             |
| K 线         | 普通 K 线接口最多返回最近 1,000 根 | 参数校验及历史 K 线独立接口    |
| 行情权限     | 根据市场、标的及订阅等级变化       | 由 Longbridge 账户权限决定     |

官方 SDK 已对行情接口实现频率控制，调用速度过快时会主动延迟。网关增加限流主要是为了保护服务和控制请求队列，而不是重新实现全部上游限流逻辑。

**核心原则：只查询行情，不支持交易。**

系统不创建交易上下文，不暴露下单、撤单、资金划转、账户资产等接口，避免将行情网关变成具有交易权限的服务。

## **二、API 接口设计**

建议统一使用 `/api/v1` 前缀，所有业务接口使用 `GET` 方法。

### **2.1 第一阶段支持的接口**

| HTTP API                           | 功能             | 官方 SDK 方法                 | 优先级 |
| ---------------------------------- | ---------------- | ----------------------------- | ------ |
| `GET /api/v1/quotes`               | 批量查询最新报价 | `Quote()`                     | P0     |
| `GET /api/v1/quotes/{symbol}`      | 单只股票报价     | `Quote()`                     | P0     |
| `GET /api/v1/securities`           | 查询证券基本信息 | `StaticInfo()`                | P0     |
| `GET /api/v1/candlesticks`         | 查询最新 K 线    | `Candlesticks()`              | P0     |
| `GET /api/v1/candlesticks/history` | 查询历史 K 线    | `HistoryCandlesticksByDate()` | P1     |
| `GET /api/v1/intraday`             | 查询当日分时     | `Intraday()`                  | P1     |
| `GET /api/v1/depth`                | 查询买卖盘口     | `Depth()`                     | P1     |
| `GET /api/v1/trades`               | 查询逐笔成交     | `Trades()`                    | P1     |
| `GET /api/v1/calc-indexes`         | 查询计算指标     | `CalcIndex()`                 | P1     |
| `GET /api/v1/trading-sessions`     | 查询交易时段     | `TradingSession()`            | P2     |
| `GET /api/v1/trading-days`         | 查询交易日历     | `TradingDays()`               | P2     |

以上 SDK 方法已在 Longbridge 官方 Go SDK 中提供。

第二阶段还可以封装期权链、期权报价、轮证报价、资金流向及经纪队列查询。接口统一经过鉴权和缓存，内部不需要修改 Longbridge SDK。

### **2.2 股票代码规范**

直接沿用 Longbridge 的证券代码格式，不自行转换成其他数据源的代码。

| 市场     | 示例        | 含义     |
| -------- | ----------- | -------- |
| 美股     | `AAPL.US`   | 苹果     |
| 美股     | `TSLA.US`   | 特斯拉   |
| 港股     | `700.HK`    | 腾讯     |
| A 股上海 | `600519.SH` | 贵州茅台 |
| A 股深圳 | `000001.SZ` | 平安银行 |
| ETF      | `QQQM.US`   | 美股 ETF |

新加坡市场的实时行情目前未通过 Longbridge Developers API 开放，因此不将 `.SG` 纳入默认支持范围。

处理规则：

- 自动将股票代码转换成大写。
- 去掉首尾空格。
- 批量查询时去重，按照标的首次出现的顺序返回成功结果和错误列表。
- 校验市场后缀及股票代码格式。
- 不认识的代码交由上游确认，不自行猜测。
- 空列表或空标的返回 400；单次批量查询按去重前的标的数量检查上限，默认最多 50 个，配置不得超过对应上游接口的硬限制。

### **2.3 最新行情查询**

**接口**

```
GET /api/v1/quotes?symbols=AAPL.US
Authorization: Bearer YOUR_GATEWAY_TOKEN
```

支持一次请求多个标的，例如 `symbols=AAPL.US,TSLA.US,NVDA.US`；下面的响应对应只查询 `AAPL.US`。

**响应示例**

以下为网关自己定义的 JSON 格式，行情数值仅用于演示，并非真实行情。

```json
{
  "code": "OK",
  "message": "success",
  "data": [
    {
      "symbol": "AAPL.US",
      "last_done": "268.50",
      "prev_close": "266.20",
      "open": "267.00",
      "high": "270.10",
      "low": "265.90",
      "volume": 51200000,
      "turnover": "13747200000",
      "timestamp": 1794513600,
      "change": "2.30",
      "change_percent": "0.86",
      "pre_market_quote": null,
      "post_market_quote": null,
      "overnight_quote": null
    }
  ],
  "errors": [],
  "request_id": "req-example-001",
  "meta": {
    "cache_hit": false,
    "symbols": {
      "AAPL.US": {
        "cache_hit": false,
        "stored": true,
        "cached_at": "2026-11-13T11:00:00Z",
        "expires_at": "2026-11-13T11:01:00Z",
        "ttl_seconds": 60
      }
    }
  }
}
```

字段设计原则：

1. 价格、成交额、涨跌金额使用 **字符串**，避免浮点数精度问题。
2. 成交量使用整数。
3. `timestamp` 原样保留 Longbridge 的 UTC Unix 时间戳。
4. 额外计算 `change` 和 `change_percent`，方便直接展示。
5. 保留盘前、盘后、夜盘行情结构，不将不同交易时段的价格混为一谈。
6. 缓存元信息放到 `meta` 中，和行情数据分离。

涨跌字段统一按盘中最新价与昨收价计算：`change = last_done - prev_close`，`change_percent = change / prev_close × 100`。百分比的字符串不带 `%`，保留两位小数，采用四舍五入（恰好一半时远离零）；涨跌金额保留两个输入价格中较多的小数位。整个计算过程使用十进制定点或 decimal 类型，禁止先转换为浮点数。当输入缺失时，相关衍生字段返回 `null`；当 `prev_close <= 0` 时，`change_percent` 返回 `null`。盘前、盘后和夜盘数据单独保留，不用于替换上述计算的基准。

官方报价包含最新价、昨收价、开高低、成交量、成交额、交易状态及盘前盘后等字段。涨跌幅属于网关的衍生计算字段。

需要特别区分两个时间：

- `timestamp`：Longbridge 返回的市场数据时间。
- `cached_at`：网关成功获取并写入缓存的时间。

即使一个响应刚刚更新过缓存，也不代表市场价格一定是最新交易价格。例如，非交易时段获得的报价可能是上一交易日的最后成交价。

### **2.4 单只股票行情**

```
curl "http://localhost:12280/api/v1/quotes/AAPL.US" \
  -H "Authorization: Bearer YOUR_GATEWAY_TOKEN"
```

返回结构与批量接口保持一致，但成功时 `data` 是单个对象，而不是数组；失败时为 `null`，使用对应的 HTTP 状态码。`request_id` 始终在顶层，缓存信息仍放在 `meta.symbols[symbol]`。

单只股票和批量股票报价共用底层行情缓存，避免重复调用。

### **2.5 K 线接口**

```
GET /api/v1/candlesticks?symbol=AAPL.US&period=1d&count=100&adjust=none
```

参数定义：

| 参数     | 类型   | 默认值 | 说明                |
| -------- | ------ | ------ | ------------------- |
| `symbol` | string | 必填   | 股票代码            |
| `period` | string | `1d`   | K 线周期            |
| `count`  | int    | `100`  | K 线数量，最大 1000 |
| `adjust` | string | `none` | 复权方式            |

建议先支持的周期：

`1m`、`5m`、`15m`、`30m`、`1d`、`1w`、`1mo`、`1y`。

其中 `1mo` 表示月 K 线，避免与 `1m` 的一分钟 K 线混淆。

复权参数采用对外友好的字符串，由适配层映射到 SDK 枚举：

- `none`：不复权。
- `forward`：前复权。

普通 K 线接口最多返回最近 1000 根。历史查询接口与普通 K 线接口分开，避免调用方误以为普通接口能够获取任意时间范围的数据。

**历史 K 线**

```
GET /api/v1/candlesticks/history?symbol=AAPL.US&period=1d&start=2026-01-01&end=2026-06-30
```

本接口要求同时提供 `start`、`end`，格式为 `YYYY-MM-DD`，且 `start <= end`；日期按标的所在市场的交易日解释，起止日期均包含在查询范围内。`period`、`adjust` 的取值和默认值与普通 K 线接口一致。首版普通及历史 K 线均固定查询盘中时段；开启夜盘报价不会隐式改变 K 线的时段。未来增加时段参数时，必须同时扩展缓存 Key。

上游按日期区间查询最多返回 1000 根 K 线；同时指定起止日期、区间超过 1000 根时，会优先返回靠近结束日期的 1000 根，不能依靠上游报错判断数据是否完整。网关返回的数据按 `timestamp` 升序排列，并在顶层 `meta` 中增加以下字段：

| 字段                                | 语义                                                 |
| ----------------------------------- | ---------------------------------------------------- |
| `requested_start` / `requested_end` | 调用方请求的起止日期                                 |
| `returned_start` / `returned_end`   | 实际首尾 K 线的 UTC RFC 3339 时间；空结果时为 `null` |
| `returned_count`                    | 实际返回根数                                         |
| `possibly_truncated`                | 返回达到上游 1000 根上限时为 `true`，否则为 `false`  |

`possibly_truncated=true` 表示无法确认区间完整，即使区间恰好只有 1000 根也保守标记。首版不自动分页，也不承诺一次响应覆盖完整区间；调用方需要完整数据时，应缩小日期区间后重新查询，合并时按 `timestamp` 去重。分钟线在单个交易日内也可能达到上限，因此只按整日拆分不能保证完整；后续可提供基于时间偏移的分页接口。若调整网关数量配置，`MAX_CANDLESTICKS` 只用于普通 K 线的 `count` 校验，历史接口继续按上游硬上限判断是否可能截断。

依据：[Longbridge 历史 K 线查询规则](https://open.longbridge.com/zh-CN/docs/quote/pull/history-candlestick)。缓存命中时也必须保留这些范围和截断信息。

另外，官方文档对 K 线查询设置了按账户等级区分的每月标的配额，因此缓存虽然有助于降低调用次数，但不能替代对配额的管理。

### **2.6 盘口、逐笔和分时接口**

三个接口使用相同的风格：

```
GET /api/v1/depth?symbol=TSLA.US
GET /api/v1/trades?symbol=AAPL.US&count=100
GET /api/v1/intraday?symbol=700.HK
```

其中逐笔成交数量建议限制为 `1–1000`。

由于 Longbridge 的美股和港股 Level 2 权限需要单独确认，盘口接口应当返回账户实际拥有的数据，而不是承诺始终返回十档买卖盘。

对于没有权限的数据，返回明确的权限错误，不伪造或补全缺失盘口数据。

### **2.7 批量结果与部分失败**

批量报价及证券基本信息接口统一采用以下约定：

| 情况                                                 | HTTP 状态                 | 顶层 `code`       | `data` / `errors`             |
| ---------------------------------------------------- | ------------------------- | ----------------- | ----------------------------- |
| 所有标的成功                                         | 200                       | `OK`              | 成功对象数组 / 空数组         |
| 部分标的成功                                         | 200                       | `PARTIAL_SUCCESS` | 成功对象数组 / 逐标的错误数组 |
| 所有标的失败                                         | 502                       | `BATCH_FAILED`    | 空数组 / 逐标的错误数组       |
| 请求整体未通过鉴权、参数校验、入口限流或在途容量检查 | 对应的 401、400、429、503 | 对应错误码        | `null` / 空数组               |

每个错误对象包含 `symbol`、`code`、`message`、`http_status`；其中 `http_status` 表示该标的失败的分类，不改变批量 HTTP 状态。批量处理的整体 10 秒期限耗尽时，将仍未完成的标的归为 `REQUEST_TIMEOUT`（504），已完成结果按上表返回；若响应已因客户端断开而无法发送，则只结束等待。

规范化、去重后的每个标的必须恰好出现在成功结果或错误列表之一，两个数组分别保持输入顺序。上游整批调用失败时，将错误分配给该调用涉及的所有标的；不影响其他已命中缓存的标的。上游成功返回但遗漏某个标的时，使用 `SYMBOL_DATA_MISSING`（502），不能直接推断为标的不存在；只有明确的上游不存在错误才能映射为 `SYMBOL_NOT_FOUND`（404）。

成功数据按标的分别写入缓存，错误不缓存；客户端不得只根据 HTTP 200 判断所有标的成功，还须检查 `code` 与 `errors`。共享上游任务返回后，由每个 HTTP 请求独立组装结果、缓存命中标记和 `request_id`。接口的完整信封格式见 7.1。

## **三、Token 鉴权设计**

这里需要区分 **网关 Token** 和 **Longbridge Token**。

### **3.1 两层鉴权**

```mermaid
flowchart LR
    client["客户端"] -->|Gateway Token| gateway["行情网关"]
    gateway -->|Longbridge 凭证| longbridge["Longbridge"]
```

客户端永远不需要知道 Longbridge 的认证凭证。

**第一层：客户端访问网关**

使用服务自己生成的静态 Token，例如：

```
GATEWAY_TOKEN=REPLACE_WITH_A_RANDOM_SECRET
```

请求格式：

```
Authorization: Bearer REPLACE_WITH_A_RANDOM_SECRET
```

服务使用常量时间比较，避免普通字符串比较带来的时序信息泄露。

不需要 JWT、OAuth 登录、用户管理、数据库或者 Token 签发接口。

**第二层：网关访问 Longbridge**

Longbridge 官方支持两种方式：

| 认证方式     | 特点                                | 建议         |
| ------------ | ----------------------------------- | ------------ |
| OAuth 2.0    | 授权后 SDK 持久化 Token，并自动刷新 | 长期使用推荐 |
| 传统 API Key | App Key + App Secret + Access Token | 首版快速部署 |

Longbridge 当前文档推荐 OAuth 2.0，传统 API Key 作为兼容方案。

为了快速完成第一个 Docker 版本，可以先支持传统 API Key；正式长期运行时再启用 OAuth。

### **3.2 安全边界**

建议：

- 所有行情 API 都必须经过 Token 鉴权。
- 不允许通过 URL 参数传入 Token。
- Token 至少使用 32 字节随机值。
- 不将 Token 输出到日志或错误信息。
- 不提供任意 URL 的上游代理能力。
- 默认绑定本机端口；远程访问通过 HTTPS 反向代理。

生成 Token：

```
openssl rand -hex 32
```

如果部署到公网，建议增加按客户端 Token 和 IP 的速率限制，例如默认每客户端 30 QPS、突发 60。该限制针对入站流量，与上游 Longbridge 的调用限制相互独立。

## **四、缓存系统设计**

这是整个服务最重要的部分。

### **4.1 缓存要求**

**基础配置：成功获取且符合容量限制的行情数据缓存 60 秒；TTL 是最长保留时间，容量不足时允许提前淘汰。**

```
CACHE_TTL=60s
CACHE_MAX_ENTRIES=20000
CACHE_MAX_BYTES=67108864
CACHE_MAX_ITEM_BYTES=2097152
```

第一版直接使用进程内缓存，无需 Redis。

部署为一个服务实例，也符合 Longbridge 单账户长连接限制。条目数量不能代表实际内存：单条 K 线缓存可能包含 1000 根数据，因此同时限制缓存保留的负载字节数，默认 64 MiB，单条负载最多 2 MiB。

首版缓存保留不可变的序列化业务数据，不保留整份 HTTP 响应；字节预算按 Key 与业务负载的实际字节长度合计，另外用条目上限约束索引及元数据数量。写入和替换时必须同步更新预算，先清理过期项，再按 LRU 淘汰，直到同时满足条目数和字节数上限。读取时拒绝过期项，后台每 5 秒批量清理过期项；读取和序列化响应时不能修改共享缓存数据。

单项超限或无法纳入预算时跳过缓存，仍返回成功行情；该标的缓存元信息为 `stored=false`、`cached_at=null`、`expires_at=null`、`ttl_seconds=0`。此时仍合并正在执行的同 Key 查询，但后续请求可能再次访问上游。64 MiB 只约束缓存保留的 Key 和负载，不是整个进程的内存上限；SDK、HTTP 请求、临时反序列化对象和 Go GC 都需要额外空间。256 MiB 容器限额须通过包含大 K 线结果的压测验证后再确定。

### **4.2 缓存粒度**

不建议简单地使用整个 HTTP URL 作为全部缓存 Key。

例如：

```
/quotes?symbols=AAPL.US,TSLA.US
/quotes?symbols=TSLA.US,AAPL.US
/quotes?symbols=AAPL.US
```

这三个请求包含重合的股票数据。如果全部按照 URL 缓存，会重复查询上游。

建议按业务类型设计缓存 Key：

| 数据类型     | Key 示例                                    | TTL   |
| ------------ | ------------------------------------------- | ----- |
| 股票报价     | `quote:AAPL.US`                             | 60 秒 |
| 证券基本信息 | `static:AAPL.US`                            | 60 秒 |
| 盘口         | `depth:AAPL.US`                             | 60 秒 |
| 逐笔         | `trades:AAPL.US:100`                        | 60 秒 |
| 分时         | `intraday:AAPL.US`                          | 60 秒 |
| K 线         | `kline:AAPL.US:1d:100:none`                 | 60 秒 |
| 历史 K 线    | `history:AAPL.US:1d:20260101:20260630:none` | 60 秒 |

虽然证券基本信息和历史数据可以缓存更久，但按照当前需求，第一版统一 60 秒。

后续可以针对静态数据和历史数据增加不同的 TTL 策略。

### **4.3 批量查询缓存**

例如请求：

```
GET /api/v1/quotes?symbols=AAPL.US,TSLA.US,NVDA.US
```

缓存状态：

| 股票    | 缓存   |
| ------- | ------ |
| AAPL.US | 命中   |
| TSLA.US | 命中   |
| NVDA.US | 未命中 |

处理流程：

```mermaid
flowchart TD
    query["查询 3 只股票"] --> check["逐个检查缓存"]
    check --> hit["AAPL、TSLA 命中"]
    check --> miss["NVDA 未命中"]
    miss --> upstream["仅调用 Longbridge 查询 NVDA"]
    upstream --> cache["写入 NVDA 缓存"]
    hit --> merge["合并结果"]
    cache --> merge
    merge --> response["按原始请求顺序返回"]
```

这样即使不同客户端使用不同的股票组合，也可以复用已有缓存。

批量响应中的各个标的可能具有不同的 `cached_at` 和过期时间，统一放在 `meta.symbols[symbol]`，不再提供含义不明确的顶层缓存时间。单标的查询也使用相同结构；不按标的返回的接口将单份缓存元信息放在 `meta.cache`。

逐标的 `cache_hit` 表示本请求直接通过有效缓存读取取得结果；等待新上游任务（包括共享已有任务）取得结果时为 `false`。共享任务内部二次查缓存命中时，可按缓存来源标记为 `true`。`stored` 表示该数据在结果生成时已纳入缓存；缓存时间记录原始写入时间，命中不会重置。顶层 `meta.cache_hit` 仅在所有标的成功且全部命中时为 `true`；部分失败、全部失败或任一标的访问上游时均为 `false`。错误标的不出现在 `meta.symbols` 中。

### **4.4 防止缓存击穿**

假设某个时刻，100 个客户端同时请求：

```
GET /api/v1/quotes/AAPL.US
```

缓存刚好过期。

如果不采取措施，可能会生成 100 次 Longbridge 查询。

使用 Go 的：

```
golang.org/x/sync/singleflight
```

可以将同一个缓存 Key 的并发请求合并。

实际逻辑：

```
100 个请求
     |
     v
检查缓存：均未命中
     |
     v
singleflight 合并
     |
     v
共享任务内部再次检查缓存
命中则直接返回；未命中才继续
     |
     v
仅发起 1 次上游查询
     |
     v
写入缓存
     |
     v
返回给 100 个客户端
```

对批量报价，需要进一步实现**按标的合并**：

1. 为每个未命中标的按规范化后的业务 Key 查找或登记共享任务；查缓存、检查已有任务和登记新任务必须在同一 Key 的同步保护下完成。
2. 新任务执行前再次检查缓存，防止前一个任务已完成并写入缓存后，又发起重复查询。登记完成后释放锁，不能持锁进行网络调用。
3. 同时到来的同标的请求复用已有任务；新标的汇入短批处理窗口，一批最多 50 个标的（可配置，且不得超过上游 500 个标的的上限），超出则分批。
4. 每批作为一个上游任务进入有界队列，共享同一个限速器和并发池，所有行情端点都计入该上游预算。
5. 成功标的分别写入缓存，失败及遗漏标的分别记录错误；结果发布、移除共享任务须与同 Key 的缓存检查及新任务登记互斥，保证后到请求能观察到缓存或已有任务。
6. 所有完成路径，包括超时、入队失败和应用退出，都必须释放任务登记并唤醒等待者。不得遗留永远等待的任务。

可以使用一个很短的批处理窗口，例如 5 毫秒，以减少上游调用次数。

这个机制比仅对完整 URL 使用 `singleflight` 更有效。

### **4.5 TTL 语义**

明确采用：

**缓存成功写入后，经过 60 秒过期。**

例如：

| 时间     | 操作            | 结果               |
| -------- | --------------- | ------------------ |
| 12:00:00 | 第一次查询 AAPL | 调用 Longbridge    |
| 12:00:10 | 第二次查询 AAPL | 返回缓存           |
| 12:00:30 | 第三次查询 AAPL | 返回缓存           |
| 12:00:59 | 第四次查询 AAPL | 返回缓存           |
| 12:01:00 | 再次查询 AAPL   | 缓存过期，调用上游 |

缓存命中不会延长过期时间。

内部建议采用 Go 的时间机制计算 TTL，不依赖客户端时间。

### **4.6 缓存失效与异常**

默认策略：

| 情况                       | 处理                                    |
| -------------------------- | --------------------------------------- |
| 查询成功且满足缓存容量限制 | 缓存最多 60 秒，容量不足时可提前淘汰    |
| 单项超过缓存预算           | 业务成功，跳过缓存并明确 `stored=false` |
| 参数不合法                 | 不缓存                                  |
| Token 不合法               | 不缓存                                  |
| 上游权限不足               | 不写入行情缓存                          |
| 上游超时                   | 不写入行情缓存                          |
| 上游限流                   | 不写入行情缓存                          |
| 缓存过期                   | 等待新的上游结果                        |
| 服务重启                   | 内存缓存清空                            |

不建议默认返回超过 60 秒的过期数据，否则容易违反调用方对时效性的预期。

后续可增加 `stale-if-error` 选项，在上游不可用时返回旧数据，但必须明确携带 `stale: true` 和数据时间，且默认关闭。

## **五、服务内部模块设计**

建议将 HTTP 层、行情适配层、缓存层分离，方便未来扩展为其他行情供应商。

### **5.1 项目目录**

```
longbridge-quote-gateway/
├── cmd/
│   └── server/
│       └── main.go
│
├── internal/
│   ├── config/
│   │   └── config.go
│   │
│   ├── server/
│   │   ├── router.go
│   │   └── middleware.go
│   │
│   ├── handler/
│   │   ├── quote.go
│   │   ├── candlestick.go
│   │   ├── security.go
│   │   ├── depth.go
│   │   └── health.go
│   │
│   ├── service/
│   │   ├── quote_service.go
│   │   └── batching.go
│   │
│   ├── provider/
│   │   ├── interface.go
│   │   └── longbridge/
│   │       ├── client.go
│   │       ├── auth.go
│   │       └── mapper.go
│   │
│   ├── cache/
│   │   ├── cache.go
│   │   └── memory.go
│   │
│   ├── model/
│   │   ├── quote.go
│   │   └── response.go
│   │
│   └── middleware/
│       ├── auth.go
│       ├── ratelimit.go
│       └── recovery.go
│
├── docs/
│   └── openapi.yaml
│
├── Dockerfile
├── docker-compose.yml
├── .env.example
├── .dockerignore
├── go.mod
└── README.md
```

### **5.2 Provider 接口**

建议定义行情 Provider 抽象，而不是在 Handler 中直接操作 Longbridge。

示意：

```go
type SymbolQueryError struct {
    Symbol  string
    Code    string
    Message string
}

type QuoteBatchResult struct {
    Quotes []Quote
    Errors []SymbolQueryError
}

type QuoteProvider interface {
    GetQuotes(
        ctx context.Context,
        symbols []string,
    ) (QuoteBatchResult, error)

    GetCandlesticks(
        ctx context.Context,
        req CandlestickRequest,
    ) ([]Candlestick, error)

    GetHistoryCandlesticks(
        ctx context.Context,
        req HistoryRequest,
    ) ([]Candlestick, error)

    GetDepth(
        ctx context.Context,
        symbol string,
    ) (*Depth, error)

    GetIntraday(
        ctx context.Context,
        symbol string,
    ) ([]IntradayPoint, error)

    Close() error
}
```

这里的 `Quote`、`Candlestick`、`Depth` 等都是网关自定义的数据结构。

该接口为核心方法示意，其他端点按相同原则扩展。`QuoteBatchResult` 用于表达逐标的成功及失败；返回值中的 `error` 仅表示整个上游调用失败。适配层将上游遗漏标的补为 `SYMBOL_DATA_MISSING`，并确保每个请求标的恰好有一份结果或错误。HTTP 层负责按 2.7 映射状态和组装信封，缓存及请求来源信息由共享任务层维护。

Longbridge SDK 返回的对象应通过 `mapper.go` 转换成这些结构。

好处是：

- HTTP 接口不会直接依赖 SDK 的内部类型。
- SDK 升级时修改范围更小。
- 测试时可使用 Mock Provider。
- 未来接入其他行情供应商时无需重写 API。
- 可以统一处理时间、价格精度、空值和错误码。

### **5.3 Longbridge 客户端生命周期**

整个服务只维护一个 `QuoteContext`。

```mermaid
flowchart TD
    start["服务启动"] --> config["加载配置"]
    config --> credentials["初始化 Longbridge 凭证"]
    credentials --> context["创建 QuoteContext"]
    context --> connected{"建立连接成功？"}
    connected -->|是| ready["服务就绪"]
    connected -->|否| unready["进入未就绪状态"]
    unready --> reconnect["SDK 重连 / 重新初始化"]
    ready --> query["持续接受查询"]
    query --> disconnected{"连接异常？"}
    disconnected -->|否| query
    disconnected -->|是| unready
    reconnect --> connected
```

推荐使用单例模式管理连接：

```
type LongbridgeClient struct {
    ctx *quote.QuoteContext
}
```

通过官方 SDK 初始化：

```
conf, err := config.New()
if err != nil {
    return err
}

quoteCtx, err := quote.NewFromCfg(conf)
if err != nil {
    return err
}

defer quoteCtx.Close()
```

实际服务中，`Close()` 应在整个应用退出时调用，而不是每个请求完成后调用。

上述初始化及查询方法与官方 Go SDK 文档一致。

**特别注意：** 不能在每次 HTTP 请求时创建新的 `QuoteContext`，否则会产生额外连接开销，并可能触及账户的长连接限制。

连接异常后还需要：

- 检测 SDK 的自动重连结果。
- 仅在确实需要时重新创建客户端。
- 避免多个请求同时触发重新连接。
- 持续失败时采用指数退避。
- 关闭旧连接后再建立新连接。
- 在服务退出时优雅关闭。

## **六、Docker 部署设计**

### **6.1 环境变量**

建议的 `.env.example`：

```
# HTTP
HTTP_ADDR=:12280
HTTP_TIMEOUT=10s
HTTP_MAX_INFLIGHT=128

# Gateway authentication
GATEWAY_TOKEN=REPLACE_WITH_RANDOM_TOKEN

# Cache
CACHE_TTL=60s
CACHE_MAX_ENTRIES=20000
CACHE_MAX_BYTES=67108864
CACHE_MAX_ITEM_BYTES=2097152

# Upstream request limits
UPSTREAM_RPS=8
UPSTREAM_MAX_CONCURRENT=4
UPSTREAM_TIMEOUT=8s
UPSTREAM_QUEUE_SIZE=32
UPSTREAM_MAX_PENDING_KEYS=512
UPSTREAM_QUEUE_TIMEOUT=1s

# Gateway request limits
MAX_BATCH_SYMBOLS=50
MAX_CANDLESTICKS=1000

# Longbridge region
LONGBRIDGE_REGION=cn
LONGBRIDGE_LANGUAGE=zh-CN
LONGBRIDGE_ENABLE_OVERNIGHT=true

# Longbridge legacy API Key credentials
LONGBRIDGE_APP_KEY=YOUR_APP_KEY
LONGBRIDGE_APP_SECRET=YOUR_APP_SECRET
LONGBRIDGE_ACCESS_TOKEN=YOUR_ACCESS_TOKEN

# Logging
LOG_LEVEL=info
LOG_FORMAT=json
```

其中 `UPSTREAM_RPS=8` 和 `UPSTREAM_MAX_CONCURRENT=4` 是网关设计值，刻意低于 Longbridge 的上限，预留一定余量。

`HTTP_TIMEOUT` 覆盖一次 HTTP 请求从进入处理到结果生成的总时间；`UPSTREAM_TIMEOUT` 从共享任务登记开始计时，包含批处理、排队、限速及并发等待、SDK 调用。等待调度最多 1 秒，具体容量和失败行为见 7.2。配置中的数量、容量及持续时间必须为正，`UPSTREAM_RPS` 不得超过 10，`UPSTREAM_MAX_CONCURRENT` 不得超过 5，`MAX_CANDLESTICKS` 不得超过 1000；超出上游硬限制或单项缓存预算大于总预算时启动失败。

### **6.2 Dockerfile**

建议采用多阶段构建，最终使用轻量级运行镜像。

以下为项目实现后的构建模板：

```
FROM golang:1.25-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/quote-gateway \
    ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder \
    /out/quote-gateway \
    /quote-gateway

EXPOSE 12280

USER nonroot:nonroot

ENTRYPOINT ["/quote-gateway"]
```

优点是最终镜像不包含 Go 编译环境、Shell 或多余的包管理工具。

这里假定官方 SDK 及项目依赖可以使用 `CGO_ENABLED=0` 构建；开发阶段需要验证所锁定的 SDK 版本和目标 CPU 架构。

### **6.3 Docker Compose**

```
services:
  quote-gateway:
    build:
      context: .
      dockerfile: Dockerfile

    image: longbridge-quote-gateway:latest
    container_name: longbridge-quote-gateway

    restart: unless-stopped

    init: true

    ports:
      - "127.0.0.1:12280:12280"

    env_file:
      - .env

    read_only: true

    tmpfs:
      - /tmp

    security_opt:
      - no-new-privileges:true

    cap_drop:
      - ALL

    mem_limit: 256m
    cpus: 1.0

    stop_grace_period: 20s
```

此配置使用传统 API Key 模式，不需要挂载 Token 文件。

内存和 CPU 限额是首版的建议起点，后续根据实际标的数量、响应大小和并发进行调整。

### **6.4 启动**

项目代码实现后，运行：

```
cp .env.example .env
```

编辑 `.env`，设置真实凭证和网关 Token，然后执行：

```
docker compose up -d --build
```

查看日志：

```
docker compose logs -f quote-gateway
```

查询行情：

```
curl \
  "http://127.0.0.1:12280/api/v1/quotes?symbols=AAPL.US,TSLA.US" \
  -H "Authorization: Bearer YOUR_GATEWAY_TOKEN"
```

需要注意：上述 Dockerfile 和 Compose 是设计模板，并非已经构建、测试完成的项目。它们依赖前面所述的 Go 服务代码。

### **6.5 OAuth 部署方案**

虽然传统 API Key 比较容易配置，但对于长期运行的服务，推荐最终切换到 OAuth 2.0。

官方 SDK 支持 OAuth Token 持久化及自动刷新，Linux 下的默认位置是：

```
~/.longbridge/openapi/tokens/<client_id>
```

建议使用两阶段初始化：

**首次授权：** 在本机执行一个独立的 `auth` 命令，通过浏览器完成 OAuth 授权，生成本地 Token 文件。

**Docker 运行：** 将生成的文件挂载进容器，固定容器内的 `HOME` 路径，让 SDK 可以读取并刷新 Token。

例如，在容器用户的 Home 目录设置为 `/home/nonroot` 时：

```
volumes:
  - ./data/longbridge:/home/nonroot/.longbridge

environment:
  HOME: /home/nonroot
```

需要确保宿主机目录具有正确的读写权限，因为 SDK 刷新 Token 后必须能够持久化更新。

不建议让无人值守的 HTTP 服务在启动阶段临时发起浏览器授权。首次授权应该作为独立部署步骤。

## **七、接口错误处理**

### **7.1 统一响应结构**

所有业务响应始终包含 `code`、`message`、`data`、`errors`、`request_id`、`meta` 六个顶层字段。`code` 始终为字符串，成功使用 `OK`；`message` 为字符串，`errors` 始终为数组，`meta` 始终为对象。`request_id` 始终在顶层，并同步返回在 `X-Request-ID` 响应头中；服务为每个 HTTP 请求生成独立 ID，不能缓存或复用共享上游任务的 ID。

成功（以下以单只股票报价为例；价格为演示值）：

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "symbol": "AAPL.US",
    "last_done": "268.50",
    "prev_close": "266.20",
    "open": "267.00",
    "high": "270.10",
    "low": "265.90",
    "volume": 51200000,
    "turnover": "13747200000",
    "timestamp": 1794513600,
    "change": "2.30",
    "change_percent": "0.86",
    "pre_market_quote": null,
    "post_market_quote": null,
    "overnight_quote": null
  },
  "errors": [],
  "request_id": "req-example-002",
  "meta": {
    "cache_hit": true,
    "symbols": {
      "AAPL.US": {
        "cache_hit": true,
        "stored": true,
        "cached_at": "2026-11-13T11:00:00Z",
        "expires_at": "2026-11-13T11:01:00Z",
        "ttl_seconds": 60
      }
    }
  }
}
```

单标的失败（HTTP 504）：

```json
{
  "code": "UPSTREAM_TIMEOUT",
  "message": "Longbridge quote request timed out",
  "data": null,
  "errors": [],
  "request_id": "req-example-003",
  "meta": {
    "cache_hit": false
  }
}
```

批量部分失败（查询 `AAPL.US,NVDA.US`，HTTP 200）：

```json
{
  "code": "PARTIAL_SUCCESS",
  "message": "some symbols failed",
  "data": [
    {
      "symbol": "AAPL.US",
      "last_done": "268.50",
      "prev_close": "266.20",
      "open": "267.00",
      "high": "270.10",
      "low": "265.90",
      "volume": 51200000,
      "turnover": "13747200000",
      "timestamp": 1794513600,
      "change": "2.30",
      "change_percent": "0.86",
      "pre_market_quote": null,
      "post_market_quote": null,
      "overnight_quote": null
    }
  ],
  "errors": [
    {
      "symbol": "NVDA.US",
      "code": "UPSTREAM_TIMEOUT",
      "message": "Longbridge quote request timed out",
      "http_status": 504
    }
  ],
  "request_id": "req-example-004",
  "meta": {
    "cache_hit": false,
    "symbols": {
      "AAPL.US": {
        "cache_hit": true,
        "stored": true,
        "cached_at": "2026-11-13T11:00:00Z",
        "expires_at": "2026-11-13T11:01:00Z",
        "ttl_seconds": 60
      }
    }
  }
}
```

批量全部失败时使用 `code=BATCH_FAILED`、`data=[]`，`errors` 列出全部标的；其余字段结构与上例相同。整体请求在开始标的处理前被拒绝时使用 `data=null`、`errors=[]`。单标的错误由顶层 `code`、`message` 表达，无需在 `errors` 中重复。

单标的失败或整体拒绝请求时的 HTTP 状态及逐标的错误分类如下；批量完成后的 HTTP 状态以 2.7 为准：

| HTTP 状态 | 业务错误码                      | 场景                          |
| --------- | ------------------------------- | ----------------------------- |
| 400       | `INVALID_PARAMETER`             | 股票代码、周期、数量非法      |
| 401       | `UNAUTHORIZED`                  | Token 无效或缺失              |
| 403       | `MARKET_DATA_PERMISSION_DENIED` | Longbridge 行情权限不足       |
| 404       | `SYMBOL_NOT_FOUND`              | 已确认标的不存在              |
| 429       | `RATE_LIMITED`                  | 网关主动入口限流              |
| 502       | `UPSTREAM_ERROR`                | Longbridge 返回异常           |
| 502       | `SYMBOL_DATA_MISSING`           | 上游成功响应中缺少请求标的    |
| 503       | `UPSTREAM_UNAVAILABLE`          | Longbridge 服务或连接不可用   |
| 503       | `UPSTREAM_QUEUE_FULL`           | 上游队列或待处理 Key 容量已满 |
| 503       | `UPSTREAM_QUEUE_TIMEOUT`        | 等待上游调度超过 1 秒         |
| 503       | `REQUEST_CAPACITY_EXCEEDED`     | HTTP 在途请求容量已满         |
| 504       | `UPSTREAM_TIMEOUT`              | 共享上游任务总预算耗尽        |
| 504       | `REQUEST_TIMEOUT`               | HTTP 请求总期限耗尽           |

对 SDK 的原始错误需要建立映射表，不能根据错误字符串简单判断。例如，权限不足与标的不存在属于不同情况；不能将它们统一映射为 404。

### **7.2 超时设计**

所有行情端点共用以下预算；缓存命中不占用上游队列和调用次数：

| 环节               | 默认约定                                             |
| ------------------ | ---------------------------------------------------- |
| HTTP 请求总超时    | 10 秒，包含鉴权、缓存、共享任务等待和结果组装        |
| HTTP 在途请求上限  | 128 个，包括等待同一共享任务的各个 HTTP 请求         |
| 共享上游任务总超时 | 8 秒，从任务登记开始，覆盖整个上游处理过程           |
| 等待调度超时       | 1 秒，包含批处理窗口、队列、限速器和并发池等待       |
| 上游调用并发上限   | 4 个；批量调用计为 1 个调用                          |
| 上游请求频率       | 8 次/秒，平滑放行，突发容量为 1                      |
| 上游排队容量       | 32 个待执行调用批次，不包括正在调用 SDK 的批次       |
| 待处理 Key 上限    | 512 个唯一业务 Key，覆盖批处理窗口、排队和执行中任务 |
| 容量不足           | 立即拒绝新增工作，不创建额外等待 goroutine           |
| 客户端取消请求     | 立即退出该客户端的等待，不取消其他等待者仍需的任务   |

`HTTP_MAX_INFLIGHT` 在进入业务处理时无等待地获取名额，满额返回 503 `REQUEST_CAPACITY_EXCEEDED`。Key 上限、队列上限分别限制积压数据量及调用数量；原子登记新 Key 时若容量不足，不保留半完成的登记，并将该标的记录为 `UPSTREAM_QUEUE_FULL`。已存在的同 Key 任务可以继续被复用，但每个 HTTP 请求仍受在途请求上限约束。

共享任务的 8 秒截止时间和 1 秒调度截止时间在首次登记时固定，后到等待者不能延长。合并成一批的 Key 使用其中最早的截止时间；从调度阶段进入 SDK 调用时使用剩余的总预算，不能重新获得完整 8 秒。调度阶段耗尽 1 秒时使用 `UPSTREAM_QUEUE_TIMEOUT`；SDK 调用耗尽共享任务总预算时使用 `UPSTREAM_TIMEOUT`。SDK 内部的限速等待也必须受传入 Context 约束，并通过实现测试验证。

上游队列满、等待调度超时等错误发生在标的任务层时，按 2.7 组装批量结果；不要因为一个标的排队失败就丢弃其他已成功的结果。单标的请求使用 7.1 中对应状态码。HTTP 10 秒期限到达时，单标的返回 504 `REQUEST_TIMEOUT`，批量将未完成项标记为该错误并保留已完成结果；HTTP 写出还需配置有限的网络写超时。

由于多个客户端可以共用同一个上游请求，不能将首个客户端的请求 Context 用作共享任务的完整生命周期。共享任务从应用生命周期 Context 派生，并持有上述独立截止时间。客户端取消时只退出自己的等待过程；首版允许已登记任务继续运行直到完成或超时，以便写入缓存，最多保留 8 秒。应用退出时取消所有共享任务，清理登记并唤醒等待者；不得使用没有超时和退出控制的后台 Context。

## **八、监控与运行维护**

### **8.1 健康检查**

提供两个不需要业务 Token 的内部检查接口。

**存活检查**

```
GET /healthz
```

表示 HTTP 服务进程仍然正常工作。

**就绪检查**

```
GET /readyz
```

表示配置有效且行情客户端处于可用状态。

不建议每次执行健康检查时都请求 Longbridge，否则监控自身会消耗上游调用次数。

可以通过最近一次连接状态、初始化结果和 SDK 提供的状态信息判断就绪情况。

### **8.2 监控指标**

建议至少记录：

| 指标                                | 说明                              |
| ----------------------------------- | --------------------------------- |
| `http_requests_total`               | HTTP 请求总数                     |
| `http_request_duration_seconds`     | 请求耗时                          |
| `cache_hits_total`                  | 缓存命中次数                      |
| `cache_misses_total`                | 缓存未命中次数                    |
| `cache_entries`                     | 当前缓存条目数                    |
| `cache_bytes`                       | 缓存 Key 与业务负载字节数         |
| `cache_evictions_total`             | 按原因区分的缓存淘汰次数          |
| `cache_skipped_total`               | 单项超限等原因导致的未缓存次数    |
| `upstream_requests_total`           | Longbridge 调用次数               |
| `upstream_request_duration_seconds` | 上游查询耗时                      |
| `upstream_errors_total`             | 上游错误数                        |
| `upstream_inflight`                 | 正在执行的上游请求                |
| `upstream_queue_length`             | 等待调度的调用批次数              |
| `upstream_pending_keys`             | 已登记且尚未完成的唯一业务 Key 数 |
| `upstream_connected`                | 长连接状态                        |

可以将 `/metrics` 限制为本机或内网访问，后续接入 Prometheus。

初期只使用 JSON 日志也可以，不必为了监控而引入额外容器。

### **8.3 服务日志**

正常请求日志建议包含：

```
{
  "level": "info",
  "method": "GET",
  "path": "/api/v1/quotes",
  "status": 200,
  "symbols_count": 3,
  "cache_hit_count": 2,
  "cache_miss_count": 1,
  "upstream_requests": 1,
  "latency_ms": 85
}
```

不要记录：

- 完整的 Authorization Header。
- Longbridge Access Token。
- App Secret。
- OAuth Refresh Token。

## **九、部署到现有家庭服务器**

如果已经使用 Caddy 管理 HTTPS 服务，建议直接利用现有反向代理，不必再部署 Nginx。

网络结构：

```mermaid
flowchart LR
    client["外部客户端"] -->|HTTPS| caddy["Caddy"]
    caddy -->|HTTP / 内网| gateway["Quote Gateway :12280"]
    gateway -->|WebSocket / HTTPS| longbridge["Longbridge"]
```

例如，将服务部署到一个独立的子域名：

```
https://quote.h.example.com
```

Caddyfile：

```
quote.h.example.com {
    encode zstd gzip

    reverse_proxy 127.0.0.1:12280
}
```

前提是 Caddy 与行情网关运行在同一主机，并且证书管理已配置完成。

如果 Caddy 与网关运行在不同的容器中，则应放在同一个 Docker 网络，并使用容器服务名作为反向代理目标，而不是 `127.0.0.1`。

对外请求：

```
curl \
  "https://quote.h.example.com/api/v1/quotes/AAPL.US" \
  -H "Authorization: Bearer YOUR_GATEWAY_TOKEN"
```

不需要暴露额外的管理端口。

**安全建议：** 如果这个网关仅供个人使用，可优先通过内网或 VPN 暴露。若允许公网调用，必须启用 HTTPS、Token 鉴权和入口限流。

还应确认 Longbridge 行情数据的授权协议是否允许向第三方用户再分发。个人私有网关和公开市场数据服务可能适用不同的授权要求。

## **十、开发与测试计划**

建议按三个阶段实现。

| 阶段         | 开发内容                                                                                            | 完成标准                                 |
| ------------ | --------------------------------------------------------------------------------------------------- | ---------------------------------------- |
| P0：基础可用 | Token 鉴权、最新报价、基本信息、K 线、有界缓存及队列、按 Key 合并、统一响应、Docker、核心自动化测试 | 能通过 curl 查询行情，缓存及容量边界正确 |
| P1：完善行情 | 历史 K 线、盘口、逐笔、分时、指标、批处理窗口调优                                                   | 支持主要行情查询，减少重复请求           |
| P2：工程增强 | OAuth、监控、入口限流、重连策略增强、Swagger、扩展自动化测试                                        | 能稳定长期运行                           |

### **10.1 核心验收测试**

| 测试           | 操作                                           | 预期                                                    |
| -------------- | ---------------------------------------------- | ------------------------------------------------------- |
| 鉴权测试       | 不带 Token 请求                                | 返回 401                                                |
| 鉴权测试       | 使用错误 Token                                 | 返回 401                                                |
| 首次查询       | 请求 AAPL.US                                   | 调用上游一次                                            |
| 缓存命中       | 60 秒内重复请求                                | 不调用上游                                              |
| 缓存过期       | 60 秒后请求                                    | 重新查询上游                                            |
| 批量缓存       | 查询部分已缓存的标的                           | 只请求缺失标的                                          |
| 并发合并       | 100 个并发请求同一股票，查询成功且可缓存       | 仅调用上游一次                                          |
| 缓存竞争       | 请求首次未命中后暂停，另一请求查询并写入后恢复 | 共享任务二次检查命中，不重复查询上游                    |
| 交叉批量       | 并发查询 AAPL/TSLA 和 TSLA/NVDA                | TSLA 同时最多存在一个共享任务                           |
| 部分失败       | AAPL 命中缓存、NVDA 上游超时                   | HTTP 200、`PARTIAL_SUCCESS`，保留 AAPL 并列出 NVDA 错误 |
| 全部失败       | 所有批量标的均失败                             | HTTP 502、`BATCH_FAILED`，逐标的错误完整                |
| 上游遗漏       | 上游成功但少返回一个标的                       | 缺失项为 `SYMBOL_DATA_MISSING`，不伪造 404              |
| 响应一致性     | 分别校验成功、部分失败和失败响应               | `code` 为字符串，`request_id` 在顶层，六个信封字段齐全  |
| 混合缓存时间   | 返回两个不同写入时间的标的                     | 逐标的时间准确，顶层不提供单一缓存时间                  |
| 衍生字段       | 涨跌金额及百分比含舍入边界、缺值或零昨收       | 使用 decimal 规则，百分比无除零，缺值返回 `null`        |
| 历史截断（P1） | 模拟返回 999、1000 根历史 K 线及空结果         | 截断标记分别为 false、true、false，范围信息准确         |
| 缓存容量       | 写入不同大小的 K 线结果直至达到预算            | 先清理过期项再 LRU 淘汰，字节及条目预算均不超限         |
| 大项缓存       | 单项超过 2 MiB                                 | 业务成功，`stored=false` 且缓存时间为 `null`            |
| HTTP 容量      | 占满 128 个在途请求后继续请求                  | 立即返回 503 `REQUEST_CAPACITY_EXCEEDED`                |
| 上游容量       | 分别占满队列或唯一 Key 容量                    | 新任务报 `UPSTREAM_QUEUE_FULL`，无登记泄漏              |
| 调度等待       | 限速或并发等待超过 1 秒                        | `UPSTREAM_QUEUE_TIMEOUT`，不再调用 SDK                  |
| 总预算         | 模拟排队和 SDK 调用、HTTP 等待分别超时         | 不重置截止时间，使用对应错误并唤醒所有等待者            |
| 取消等待       | 首个客户端取消、第二个仍等待同一任务           | 第二个仍能取得结果，共享任务不被首个客户端取消          |
| 参数校验       | 单次查询超过 50 个标的                         | 返回 400                                                |
| 上游限流       | 模拟大量不同股票查询                           | 不超过配置的频率                                        |
| 上游断线       | 模拟 Longbridge 连接异常                       | 返回适当错误并尝试恢复                                  |
| 容器重启       | 重启 Docker 服务                               | 能重新初始化连接和缓存                                  |

对于并发缓存测试，重点验证的是**每个规范化业务 Key 同一时刻最多存在一个共享查询任务**；成功结果写入后，只要该项仍在有效缓存中，新请求不得重复访问上游。错误或超大结果不缓存，后续查询允许再次请求上游；提前淘汰也允许重新查询。

### **10.2 性能目标**

以下是建议的工程验收目标，不是已经测得的性能结果。

| 项目               | 目标                         |
| ------------------ | ---------------------------- |
| 已缓存行情查询 P95 | 10 ms 以内（服务端处理耗时） |
| 正常缓存命中率     | 80% 以上（取决于访问模式）   |
| 同 Key 并发请求    | 合并为一次上游查询           |
| 上游最大并发       | 4                            |
| 上游调用频率       | 不超过 8 次/秒               |
| 稳态内存占用       | 优先控制在 256 MB 内         |
| 实例数量           | 1                            |

正常使用时，实际缓存命中率可能非常高。例如多个策略程序、脚本和仪表盘频繁查询同一批美股时，缓存可以明显降低对 Longbridge 的调用压力。

但如果每次请求的股票都不同，缓存命中率会降低，不能仅依靠缓存保证所有请求的响应时间。

## **十一、几个需要提前确定的技术细节**

### **11.1 是否要引入 Redis？**

**第一版不需要。**

当前架构为单实例、单 Longbridge 连接，内存缓存的优势明显：

- 延迟更低。
- 实现更简单。
- 不增加额外容器。
- 不需要网络序列化。
- 不需要 Redis 数据持久化。

只有未来需要多实例、跨服务共享缓存时，才考虑 Redis。

而多实例部署还需要先解决 Longbridge 单账户长连接限制，不能直接将服务水平扩容成多个独立的 `QuoteContext`。

### **11.2 是否直接使用 Longbridge WebSocket 推送更新缓存？**

暂时不建议。

有两种实现方式：

| 方式                      | 优点               | 缺点                             |
| ------------------------- | ------------------ | -------------------------------- |
| 按需拉取 + 60 秒缓存      | 简单，适合低频查询 | 缓存有效期内价格可能变化         |
| WebSocket 订阅 + 本地缓存 | 可持续接收实时更新 | 需要订阅管理、断线重连、权限处理 |

目前要求仅为查询行情，且允许一分钟缓存，因此采用按需拉取最合适。

未来如果需要秒级更新、自选股实时仪表盘或者 WebSocket API，可以添加订阅模块。

### **11.3 缓存是否应该支持手动刷新？**

**建议第一版不开放普通客户端的强制刷新功能。**

例如，不支持通过：

```
?force_refresh=true
```

绕过缓存。

因为一旦允许任意客户端跳过缓存，就可能出现大量重复调用，使缓存失去意义。

如果以后确实有需要，可以增加单独的管理权限，并对强制刷新做严格限流。

### **11.4 SDK 版本管理**

官方 Go SDK 仍在持续迭代，建议：

- 在 `go.mod` 中锁定确定的依赖版本。
- 提交 `go.sum`。
- 不在 Docker 构建时自动获取最新 SDK。
- 每次升级 SDK 时运行接口兼容性测试。
- 避免将 SDK 原始 JSON 直接当成对外 API 契约。

目前官方 Go SDK 已覆盖这套方案主要需要的行情查询方法。

## **十二、最终推荐的 MVP 规格**

综合上述设计，我建议第一版采用以下配置：

| 维度           | 最终方案                                       |
| -------------- | ---------------------------------------------- |
| 项目名称       | Longbridge Quote Gateway                       |
| 语言           | Go                                             |
| HTTP 框架      | Gin                                            |
| 上游           | Longbridge 官方 Go SDK                         |
| 对外协议       | REST JSON                                      |
| 对外认证       | 单静态 Bearer Token                            |
| 上游认证       | 初期 API Key，后续 OAuth 2.0                   |
| 缓存           | 进程内缓存，TTL 60 秒                          |
| 缓存方式       | 按标的缓存 + singleflight                      |
| 默认接口       | 报价、证券信息、K 线                           |
| 扩展接口       | 历史 K 线、分时、盘口、逐笔、计算指标          |
| 最大单次标的数 | 50                                             |
| 最大上游并发   | 4                                              |
| 上游请求频率   | 8 次/秒                                        |
| 缓存负载预算   | 64 MiB，单项最多 2 MiB，LRU 淘汰               |
| HTTP 在途上限  | 128 个请求                                     |
| 上游排队上限   | 32 个调用批次、512 个待处理 Key，等待最多 1 秒 |
| 数据存储       | 不需要数据库                                   |
| 部署方式       | 单 Docker 容器                                 |
| HTTPS          | 复用 Caddy                                     |
| 服务运行       | 单实例、单 `QuoteContext`                      |
| 资源配置       | 1 vCPU、256 MB 内存起步                        |

**最重要的设计决策是：将服务定位为一个轻量、只读、带一分钟缓存的 Longbridge API Gateway，而不是重新实现一个行情系统。**

这样能够尽量利用官方 SDK 提供的认证、连接管理和行情请求能力，将自己需要维护的代码集中在 REST API、缓存、Token 鉴权以及容器部署四个部分。

官方参考资料：

- [Longbridge OpenAPI 文档](https://open.longbridge.com/zh-CN/docs)
- [官方 SDK 快速开始](https://open.longbridge.com/zh-CN/docs/getting-started)
- [行情接口概览](https://open.longbridge.com/zh-CN/docs/quote/overview)
- [Go SDK Quote API](https://pkg.go.dev/github.com/longbridge/openapi-go/quote)
- [官方 Go SDK GitHub 仓库](https://github.com/longbridge/openapi-go)
