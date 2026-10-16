# HTTP API and metrics

The dashboard, CLI and TUI are all clients of this API. Back to the [README](../README.md).

## HTTP API

The dashboard is a thin client over a small JSON API.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/state` | providers, keys, settings, addresses |
| GET | `/api/logs` | recent proxied requests |
| POST | `/api/settings` | `{listen, maxRetries, cooldownSec, adminToken, proxyToken, rateLimitRPS, keyRateLimitRPS}` (`listen` applies on next start) |
| POST | `/api/autostart` | `{enabled}`: turn start-at-login on or off; returns what is installed |
| POST | `/api/path` | `{enabled}`: put `keyrotator` on PATH or take it off; returns the new state and what is left to do |
| GET | `/metrics` | Prometheus metrics (see below) |
| POST | `/api/providers` | create provider (also accepts `proxyToken`, `rateLimitRPS`, `keyRateLimitRPS`) |
| PUT / DELETE | `/api/providers/{id}` | update / delete provider |
| POST | `/api/providers/{id}/test` | health-probe every key |
| POST | `/api/providers/{id}/keys` | add a key; optional `label` (alias, defaults to `key N`), `pool` (key group, blank = `default`) and `parts` (`{"session_id": "..."}`) |
| PUT / DELETE | `/api/providers/{id}/keys/{kid}` | update (`value`, `label`, `pool`, `parts`, `enabled`) / delete a key; `parts` replaces the whole set |
| POST | `/api/providers/{id}/keys/{kid}/test` | probe one key |
| POST | `/api/providers/{id}/keys/{kid}/reset` | clear cooldown/invalid status |

If an **admin token** is set in Settings, every `/api/*` call must send
`X-Admin-Token: <token>`; the dashboard prompts for it and remembers it locally.

## Metrics

`GET /metrics` serves Prometheus text format. If an admin token is set, the scrape must send it
(`X-Admin-Token` header, or `?token=`).

| Series | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `keyrotator_requests_total` | counter | `provider`, `code` | proxied requests by upstream status (429 and 502 included) |
| `keyrotator_request_duration_seconds` | summary | `provider` | time spent proxying (`_sum`, `_count`) |
| `keyrotator_retries_total` | counter | `provider` | failovers to another key within a request |
| `keyrotator_rate_limited_total` | counter | `provider`, `scope` | refused by a local limit (`scope` is `provider` or `key`) |
| `keyrotator_keys` | gauge | `provider`, `state` | keys in each state: `ok`, `cooldown`, `invalid`, `error`, `disabled` |
| `keyrotator_providers` | gauge | | configured providers |

```yaml
# prometheus.yml
scrape_configs:
  - job_name: keyrotator
    static_configs: [{ targets: ["127.0.0.1:8787"] }]
    # authorization: { credentials: "<admin token>" }   # if an admin token is set
```


## Response headers

Every proxied response tells you what happened:

| Header | Meaning |
| --- | --- |
| `X-Rotator-Provider` | provider id |
| `X-Rotator-Key` | alias of the key that served the response |
| `X-Rotator-Key-Id` | stable id of that key |

Send `X-Rotator-Key: <alias-or-id>` on a request to pin it to one specific key.
