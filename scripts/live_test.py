#!/usr/bin/env python3
"""Read-only integration checks. Uses .env without printing credentials or quote data.
Run against one running gateway: python3 scripts/live_test.py
"""
import concurrent.futures
import datetime as dt
import decimal
import json
import os
from pathlib import Path
import shlex
import statistics
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = os.environ.get('GATEWAY_TEST_URL', 'http://127.0.0.1:12280')

def env_values():
    result = {}
    for line in Path('.env').read_text().splitlines():
        if not line.strip() or line.lstrip().startswith('#') or '=' not in line:
            continue
        key, value = line.split('=', 1)
        parts = shlex.split(value, comments=True)
        result[key.strip()] = parts[0] if parts else ''
    return result

ENV = env_values()
TOKEN = ENV['GATEWAY_TOKEN']
REPORT = []

def request(path, params=None, token=TOKEN):
    url = BASE + path
    if params:
        url += '?' + urllib.parse.urlencode(params)
    headers = {'Authorization': 'Bearer ' + token} if token else {}
    started = time.perf_counter()
    req = urllib.request.Request(url, headers=headers)
    try:
        response = urllib.request.urlopen(req, timeout=15)
    except urllib.error.HTTPError as exc:
        response = exc
    with response:
        raw = response.read()
        body = json.loads(raw) if 'application/json' in response.headers.get('Content-Type', '') else raw.decode()
        return response.status, body, (time.perf_counter() - started) * 1000

def record(name, status, detail=None):
    entry = {'test': name, 'status': status}
    if detail is not None:
        entry['detail'] = detail
    REPORT.append(entry)
    print(json.dumps(entry, ensure_ascii=False), flush=True)

def check(name, condition, detail=None):
    record(name, 'PASS' if condition else 'FAIL', detail)
    return condition

def metrics():
    status, body, _ = request('/metrics')
    assert status == 200
    return {line.split()[0]: float(line.split()[1]) for line in body.splitlines() if line and not line.startswith('#')}

def envelope(body):
    return isinstance(body, dict) and set(('code', 'message', 'data', 'errors', 'request_id', 'meta')).issubset(body) and isinstance(body['request_id'], str) and bool(body['request_id'])

def main():
    deadline = time.monotonic() + 45
    while time.monotonic() < deadline:
        try:
            if request('/readyz', token=None)[0] == 200:
                break
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(1)
    else:
        record('readiness', 'FAIL', 'not ready within 45 seconds')
        return
    check('health/readiness', request('/healthz', token=None)[0] == 200)
    for token in [None, 'invalid-test-token']:
        status, body, _ = request('/api/v1/quotes/AAPL.US', token=token)
        check('authentication', status == 401 and body['code'] == 'UNAUTHORIZED' and envelope(body))
    check('metrics protected', request('/metrics', token=None)[0] == 401)
    check('OpenAPI', len(request('/openapi.json')[1]['paths']) >= 13)
    today = dt.date.today()
    start, end = (today-dt.timedelta(days=14)).isoformat(), today.isoformat()
    cases = [
        ('quotes', '/api/v1/quotes', {'symbols': ' aapl.us ,TSLA.US,AAPL.US'}),
        ('single_quote', '/api/v1/quotes/AAPL.US', {}),
        ('securities', '/api/v1/securities', {'symbols': 'AAPL.US,700.HK,600519.SH,000001.SZ'}),
        ('candlesticks', '/api/v1/candlesticks', {'symbol': 'AAPL.US', 'count': 100}),
        ('history', '/api/v1/candlesticks/history', {'symbol': 'AAPL.US', 'start': start, 'end': end}),
        ('intraday', '/api/v1/intraday', {'symbol': 'AAPL.US'}),
        ('depth', '/api/v1/depth', {'symbol': 'AAPL.US'}),
        ('trades', '/api/v1/trades', {'symbol': 'AAPL.US', 'count': 100}),
        ('calc_indexes', '/api/v1/calc-indexes', {'symbols': 'AAPL.US', 'indexes': 'last_done,change_val,change_rate,volume,turnover,turnover_rate,total_market_value,pe_ttm_ratio,pb_ratio,dividend_ratio_ttm'}),
        ('trading_sessions', '/api/v1/trading-sessions', {}),
        ('trading_days', '/api/v1/trading-days', {'market': 'US', 'start': start, 'end': end}),
    ]
    quotes = {}
    for name, path, params in cases:
        time.sleep(.15)
        before = metrics()['upstream_requests_total']
        status, body, ms = request(path, params)
        check(name + ': envelope', envelope(body))
        if status == 403 or (body.get('errors') and all(e['code'] == 'MARKET_DATA_PERMISSION_DENIED' for e in body['errors'])):
            record(name, 'ACCOUNT_LIMITATION', {'http': status, 'code': body['code']})
            continue
        if not check(name, status == 200 and body['code'] == 'OK', {'http': status, 'code': body['code'], 'latency_ms': round(ms, 2), 'errors': [e['code'] for e in body.get('errors', [])]}):
            continue
        after = metrics()['upstream_requests_total']
        _, cached, _ = request(path, params)
        check(name + ': cache', cached['meta']['cache_hit'] and cached['data'] == body['data'] and metrics()['upstream_requests_total'] == after)
        if name == 'quotes':
            check('normalization/order/dedup', [v['symbol'] for v in body['data']] == ['AAPL.US', 'TSLA.US'])
            quotes = {v['symbol']: v for v in body['data']}
        if name == 'single_quote':
            check('single/batch cache shared', after == before and body['meta']['cache_hit'])
        if name in ('history', 'candlesticks'):
            stamps = [v['timestamp'] for v in body['data']]
            check(name + ': ascending timestamps', stamps == sorted(stamps))
            if name == 'history':
                meta = body['meta']
                check('history range/truncation', meta['returned_count'] == len(stamps) and meta['possibly_truncated'] == (len(stamps) >= 1000) and meta['requested_start'] == start)
                check('history cached metadata', all(cached['meta'][k] == meta[k] for k in ['returned_count','possibly_truncated','returned_start','returned_end','requested_start','requested_end']))
    if 'AAPL.US' in quotes:
        q = quotes['AAPL.US']
        last, prev = decimal.Decimal(q['last_done']), decimal.Decimal(q['prev_close'])
        expected = ((last-prev)/prev*100).quantize(decimal.Decimal('.01'), rounding=decimal.ROUND_HALF_UP)
        check('decimal price/change', isinstance(q['last_done'], str) and decimal.Decimal(q['change']) == last-prev and decimal.Decimal(q['change_percent']) == expected)
    for symbol in ['700.HK','600519.SH','000001.SZ']:
        status, body, _ = request('/api/v1/quotes/'+symbol)
        record('market '+symbol, 'PASS' if status==200 else 'ACCOUNT_LIMITATION' if status==403 else 'FAIL', {'http':status, 'code':body['code']})
    invalid = [('/api/v1/quotes', {'symbols': 'AAPL.US,,TSLA.US'}),('/api/v1/quotes', {'symbols': ','.join(['AAPL.US']*501)}),('/api/v1/quotes', {'symbols':'AAPL.SG'}),('/api/v1/candlesticks',{'symbol':'AAPL.US','count':1001}),('/api/v1/candlesticks',{'symbol':'AAPL.US','period':'1M'}),('/api/v1/candlesticks/history',{'symbol':'AAPL.US','start':end,'end':start}),('/api/v1/quotes',{'symbols':'AAPL.US','force_refresh':'true'}),('/api/v1/quotes',{'symbols':'AAPL.US','token':'not-a-real-token'})]
    for path,params in invalid:
        status,body,_=request(path,params)
        check('invalid '+path, status==400 and body['code']=='INVALID_PARAMETER')
    # Populate good symbol first: upstream bad-symbol failure must not discard its cached result.
    request('/api/v1/quotes/AAPL.US')
    status,body,_=request('/api/v1/quotes',{'symbols':'AAPL.US,ZZZZINVALID999.US'})
    check('partial failure preserves cached success',status==200 and body['code']=='PARTIAL_SUCCESS' and body['data'][0]['symbol']=='AAPL.US' and len(body['errors'])==1, {'http':status,'code':body['code'],'errors':[e['code'] for e in body.get('errors',[])]})
    status,body,_=request('/api/v1/quotes',{'symbols':'ZZZZINVALID999.US'})
    check('all failed batch',status==502 and body['code']=='BATCH_FAILED')
    time.sleep(3)
    before=metrics()['upstream_requests_total']
    with concurrent.futures.ThreadPoolExecutor(max_workers=40) as pool:
        results=list(pool.map(lambda _:request('/api/v1/quotes/MSFT.US'),range(40)))
    after=metrics()['upstream_requests_total']
    check('40 concurrent cold requests coalesced',all(s==200 for s,_,_ in results) and after-before==1,{'upstream_calls':after-before,'success':sum(s==200 for s,_,_ in results)})
    time.sleep(3)
    with concurrent.futures.ThreadPoolExecutor(max_workers=50) as pool:
        results=list(pool.map(lambda _:request('/api/v1/quotes/MSFT.US'),range(100)))
    codes=[s for s,_,_ in results]
    check('100 request burst limited',429 in codes and all(s in (200,429) for s in codes),{str(s):codes.count(s) for s in set(codes)})
    time.sleep(3)
    times=[]
    for _ in range(25):
        status,body,ms=request('/api/v1/quotes/MSFT.US')
        if status==200 and body['meta']['cache_hit']:times.append(ms)
        time.sleep(.04)
    check('warm latency samples',len(times)==25,{'p50_ms':round(statistics.median(times),2),'p95_ms':round(sorted(times)[int(len(times)*.95)],2)})
    # Test actual configured TTL, preserving the original cache timestamp on hits.
    status,body,_=request('/api/v1/quotes/MSFT.US')
    meta=body['meta']['symbols']['MSFT.US']
    expires=dt.datetime.fromisoformat(meta['expires_at'].replace('Z','+00:00'))
    wait=max(0,(expires-dt.datetime.now(dt.timezone.utc)).total_seconds())+.2
    record('TTL expiry wait','INFO',{'seconds':round(wait,1)})
    if wait>120:
        record('TTL expiry','SKIP','configured TTL too long for this live suite')
    else:
        time.sleep(wait)
        before=metrics()['upstream_requests_total']
        status,body,_=request('/api/v1/quotes/MSFT.US')
        check('TTL expiry refetch',status==200 and not body['meta']['cache_hit'] and metrics()['upstream_requests_total']==before+1)
    m=metrics()
    check('no pending work',m['upstream_pending_keys']==0 and m['upstream_inflight']==0 and m['upstream_queue_length']==0)
    check('cache memory budget',m['cache_bytes']<=int(ENV.get('CACHE_MAX_BYTES',67108864)))

if __name__=='__main__':
    try:
        main()
    except Exception as exc:
        # Do not print arbitrary exceptions, URLs or credentials.
        record('suite exception','FAIL',type(exc).__name__)
    finally:
        Path('live-test-results.json').write_text(json.dumps({'time':dt.datetime.now(dt.timezone.utc).isoformat(),'checks':REPORT},ensure_ascii=False,indent=2)+'\n')
    raise SystemExit(1 if any(x['status']=='FAIL' for x in REPORT) else 0)
