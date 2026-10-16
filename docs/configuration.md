# Configuration

How keys are chosen, where they are sent, and how to secure and limit traffic.
Back to the [README](../README.md).

## How selection works

For each request the rotator builds a candidate pool of enabled keys that are not
`invalid` and not in cooldown, then picks one by the provider's strategy. On a
retryable response it marks the key and picks again, up to `maxRetries` times:

- `401` / `403` → key marked **invalid** (needs a manual reset or a passing health check)
- `429` / `5xx` / network error → key enters **cooldown** (`Retry-After` respected)
- anything else (including `400`) → key marked **ok**; the response passes straight through

Request and response bodies are streamed, so SSE/streaming completions work unchanged.

## Injecting the key (headers and query params)

Each provider has a list of **injection rules** (fields) saying what is sent where. Rules are
edited live in the dashboard (**Edit** → *Where your API key is sent* and *Fixed values*) and saved to the config as
`inject`; they take effect on the very next request.

| Field | Meaning |
| --- | --- |
| `enabled` | toggle a field without deleting it |
| `in` | `header` or `query` |
| `name` | header name or query-parameter name |
| `source` | `key` = insert the rotating pooled key · `static` = send the fixed `value` |
| `value` | the fixed value, used when `source` is `static` |
| `prefix` | text written before the value (often `Bearer `) |
| `suffix` | text written after the value |
| `group` | optional. Fields that share a group name are sent together (see below) |
| `pool` | optional, key places only. The key pool this place draws from; empty means `default` (see [Key pools](#key-pools)) |

Every field independently chooses its source, so one provider can rotate its key into several
places **and** send fixed headers at the same time. The written value is always
`prefix + value + suffix`; `prefix` and `suffix` are kept byte-for-byte, so `Bearer ` keeps its
trailing space.

```json
"inject": [
  { "enabled": true,  "in": "header", "name": "Authorization", "source": "key",    "prefix": "Bearer ", "suffix": "" },
  { "enabled": true,  "in": "query",  "name": "api_key",       "source": "key",    "prefix": "",        "suffix": "" },
  { "enabled": true,  "in": "header", "name": "x-api-version", "source": "static", "value": "2024-02-01" },
  { "enabled": false, "in": "header", "name": "x-org",         "source": "static", "value": "acme" }
]
```

### Fields that go together (groups)

Fields with **no group** are independent: each one is sent whenever it is switched on, in any
combination. Fields that share a **group name** are all-or-nothing: they are sent only when every
member is switched on and has a name. If one member is off, the whole group is held back and none of
it is sent. Group names are case-insensitive and ignore surrounding spaces, so `Beta` and ` beta `
are the same group. A key place and a fixed value can share a group.

```json
"inject": [
  { "enabled": true, "in": "header", "name": "x-api-key",   "source": "key",    "group": "tenant" },
  { "enabled": true, "in": "header", "name": "x-tenant",    "source": "static", "value": "acme", "group": "tenant" },
  { "enabled": true, "in": "header", "name": "x-trace-id",  "source": "static", "value": "debug" }
]
```

Here `x-api-key` and `x-tenant` are always sent together, and `x-trace-id` is sent on its own.
Turning off `x-tenant` holds `x-api-key` too. In the dashboard, grouped rows share a coloured rail,
and a held row is dashed and marked *held*. Groups apply to every request; they cannot yet depend on
the request path or model, and one-way dependencies (A needs B, but B can be used alone) are not
supported. Use a separate group for each bundle.

Any client-supplied value for those headers/params is replaced. Older configs written with the
single `authHeader`/`authPrefix` fields are migrated to an equivalent `inject` rule automatically
on first load, and rules with no `source` default to `key`.

## Key aliases

Every key has an **alias**: a short name such as `billing` or `team A`. Aliases make the dashboard,
logs and CLI readable, and you can pin a request to a key by its alias.

- A key added without an alias is named `key 1`, `key 2`, and so on. The lowest free number is used,
  so deleting `key 2` and adding a new key brings back `key 2`.
- Aliases are unique within a provider and compared case-insensitively. A clash returns `409`.
- Rename with the dashboard, `keyrotator alias OpenAI "key 2" billing`, the TUI's `n`, or
  `PUT /api/providers/{id}/keys/{kid}` with `{"label": "..."}`.
- To send a request with a specific key, set the `X-Rotator-Key` header to its alias or id.

## Key pools

Every key belongs to a **pool**: `default`, unless you name another one when adding it
(`keyrotator add-key OpenAI sk-... --pool partner`, or the *Pool* field in the dashboard). The
provider card shows one key table per pool. Most providers only ever need the default pool.

Each place a key is sent chooses the pool it draws from (the **Key pool** column in the editor, or
`"pool"` on an `inject` rule). On every request, KeyRotator picks one key from each pool that an
active place needs, and each pool takes turns on its own. That covers APIs that want two different
credentials on every request, such as an account key in a header and a project key in the URL.

When a request that used keys from several pools fails, the API does not say which key it rejected,
so every key that request used is marked as failed. Pinning with `X-Rotator-Key` replaces the pick for
that key's own pool only.

Round robin uses each pool's keys in list order, starting with the first, and starts again after the
last. The dashboard's preview steps through exactly that order.

## Proxy authentication

By default the `/p/` namespace is open to anything that can reach the listen address. Set a
**proxy token** (Settings → *Proxy auth token*, or `keyrotator token gen`) and every proxied
request must carry it. A provider can also define its own token, which then applies instead of
the global one.

A client may present the token in any of these ways:

| Where | Example |
| --- | --- |
| path segment (what the dashboard and CLI print) | `http://127.0.0.1:8787/p/<provider>/<token>/chat/completions` |
| rotator header | `X-Rotator-Token: <token>` |
| bearer header | `Authorization: Bearer <token>` |
| query parameter | `...?token=<token>` |

The path form is why the generated URL can be pasted straight into a client as its base URL.
Whichever form you use, the token is stripped before the request is forwarded, so your upstream
never sees it. A missing or wrong token returns `401`, and is not counted against any key.

## Rate limiting

Two independent caps, both in requests per second, both disabled at `0`:

| Setting | Scope |
| --- | --- |
| `rateLimitRPS` | all traffic into one provider, across every key |
| `keyRateLimitRPS` | traffic through a single key |

Set defaults under **Settings** and override them per provider (**Edit** — leave a field blank to
inherit). The per-key cap is checked while a key is being chosen, so a key that is over budget is
skipped and another key is used instead; only when *every* key is at its limit does the request
fail with `429` + `Retry-After`. The provider cap is checked first, so a burst is bounded before
any key is touched.

Each limit is a token bucket whose burst is `ceil(rate)`, so `5/s` allows five requests at once
and then one every 200 ms. Throttled requests are logged, and keys are never marked bad merely
because the rotator throttled them.

```json
{ "rateLimitRPS": 0, "keyRateLimitRPS": 0,
  "providers": [ { "name": "OpenAI", "rateLimitRPS": 60, "keyRateLimitRPS": 10 } ] }
```

## Security notes

- Binds to `127.0.0.1` by default. If you bind to `0.0.0.0`, set an **admin token**.
- Keys are stored **in plaintext** in the config file. Protect it (see
  [where it lives](running.md#platform-support); it is written with owner-only permissions), and
  prefer keys you can revoke.
- If `config.json` is missing, malformed, or written with a UTF-8 BOM, the app still starts: the
  bad file is moved aside to `config.json.corrupt-<timestamp>` and defaults are used. This keeps
  the service from refusing to boot on startup.
- The proxy endpoint is open to anything that can reach the listen address unless you set a
  **proxy token** (see above). Set one before binding to `0.0.0.0`.
- At startup KeyRotator logs a **warning** if it listens beyond localhost with no proxy or admin token.
- Config is saved on shutdown as well as periodically, so Ctrl+C does not lose recent changes.

