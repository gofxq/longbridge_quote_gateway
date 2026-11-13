#!/usr/bin/env python3
"""Additional read-only checks; consumes candlestick quota for AAPL.US only."""
import datetime as dt
import json
from pathlib import Path

import live_test as test


def main():
    for period in ['1m', '5m', '15m', '30m', '1d', '1w', '1mo', '1y']:
        status, body, _ = test.request('/api/v1/candlesticks', {
            'symbol': 'AAPL.US', 'period': period, 'count': 5, 'adjust': 'forward',
        })
        test.check('period ' + period, status == 200 and body['code'] == 'OK'
                   and len(body['data']) <= 5, {'http': status, 'code': body['code']})

    status, body, ms = test.request('/api/v1/candlesticks', {
        'symbol': 'AAPL.US', 'period': '1m', 'count': 1000,
    })
    test.check('1000 recent candles', status == 200 and len(body['data']) == 1000,
               {'http': status, 'latency_ms': round(ms, 2)})
    today = dt.date.today()
    params = {'symbol': 'AAPL.US', 'period': '1m',
              'start': (today - dt.timedelta(days=30)).isoformat(), 'end': today.isoformat()}
    status, body, ms = test.request('/api/v1/candlesticks/history', params)
    test.check('history 1000 truncation', status == 200 and len(body['data']) == 1000
               and body['meta']['possibly_truncated'], {'http': status, 'latency_ms': round(ms, 2)})
    if status == 200:
        _, cached, _ = test.request('/api/v1/candlesticks/history', params)
        test.check('history large cache', cached['data'] == body['data']
                   and cached['meta']['cache_hit'] and cached['meta']['possibly_truncated'])

    status, body, _ = test.request('/api/v1/trades', {'symbol': 'AAPL.US', 'count': 1000})
    test.check('1000 trades', status == 200 and len(body['data']) <= 1000, {'http': status})
    for market in ['HK', 'CN']:
        status, body, _ = test.request('/api/v1/trading-days', {
            'market': market, 'start': (today - dt.timedelta(days=14)).isoformat(),
            'end': today.isoformat(),
        })
        test.check('calendar ' + market, status == 200 and body['code'] == 'OK')


if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        test.record('suite exception', 'FAIL', type(exc).__name__)
    finally:
        Path('live-test-extended.json').write_text(
            json.dumps(test.REPORT, ensure_ascii=False, indent=2) + '\n')
    raise SystemExit(any(row['status'] == 'FAIL' for row in test.REPORT))
