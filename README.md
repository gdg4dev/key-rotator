# API Key Rotator

A single-binary, always-on local proxy that round-robins a pool of API keys.
Point any OpenAI-compatible (or custom) client at `http://127.0.0.1:8787/p/<provider>/...`
and the rotator injects the next healthy key, cools down keys that get rate-limited
or rejected, retries transparently, and shows everything in a localhost dashboard.
With a proxy token set the URL becomes `http://127.0.0.1:8787/p/<provider>/<token>/...`.

- **Local proxy** — `POST /p/<provider>/chat/completions` → upstream, with a pooled key injected.
- **Rotation** — round-robin, random, or least-recently-used, per provider.
- **Failover** — `401/403/429/5xx`/network errors mark a key bad and retry with the next key.
- **Cooldowns** — honours `Retry-After`; cool-down seconds are configurable.
- **Health checks** — a background sweeper re-tests cooled-down and invalid keys.
- **Multiple providers** — each with its own base URL, strategy and health path.
- **Flexible key injection** — put the key in any number of headers *and* query params, each
  with its own prefix/suffix (e.g. `Authorization: Bearer <key>` **and** `?api_key=<key>`),
  and mix in fixed fields that don't rotate (e.g. `x-api-version: 2024-02-01`).
- **Live configuration** — edit providers, keys and injection rules at runtime; the next
  request picks them up, no restart.
- **Localhost UI** — add keys, watch status, test keys, review live request traffic.
- **Proxy authentication** — put a token on the `/p/` URLs so only clients you hand a URL to can
  use them; one global token, optionally overridden per provider.
- **Rate limiting** — cap requests per second per provider *and* per key so you never overrun an
  upstream quota; over-limit requests get `429` + `Retry-After` without disturbing your keys.
- **Management CLI** — `keyrotator list|add|add-key|url|token|test …` drives the running instance
  from a terminal, no restart needed.
- **Windows service** — installs to start automatically at boot and restart on crash.

Everything is standard-library Go: no dependencies, no runtime, one `.exe`.

---

## Quick start

```powershell
cd keyrotator
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1     # produces bin\keyrotator.exe

.\bin\keyrotator.exe -open                                   # runs in the foreground + opens the UI
```

Then in the dashboard (`http://127.0.0.1:8787/`):

1. **+ Provider** → Name `OpenAI`, Base URL `https://api.openai.com/v1`, health path `/models`.
   The default injection rule sends `Authorization: Bearer <key>` — change it in the same form
   if your API wants something else.
2. Paste keys into **Add key** (one at a time), or seed them via the API.
3. Call it:

```powershell
curl.exe http://127.0.0.1:8787/p/<provider-id>/chat/completions `
  -H "Content-Type: application/json" `
  -d '{\"model\":\"gpt-4o-mini\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'
```

Your client never needs the API key. Response headers tell you what happened:

| Header | Meaning |
| --- | --- |
| `X-Rotator-Provider` | provider id |
| `X-Rotator-Key` | label of the key that served the response |
| `X-Rotator-Key-Id` | stable id of that key |

Send `X-Rotator-Key: <label-or-id>` on a request to pin one specific key.

---

## Run as an always-on Windows service (auto-start on boot)

Requires an **Administrator** terminal (only for install/uninstall — the app itself runs as a normal user).

```powershell
.\bin\keyrotator.exe -service install     # registers KeyRotator, start=auto, crash-restart
.\bin\keyrotator.exe -service status
.\bin\keyrotator.exe -service stop
.\bin\keyrotator.exe -service start
.\bin\keyrotator.exe -service uninstall
```

Installing writes the service to run:

```
"<path>\keyrotator.exe" -service-run -config "%APPDATA%\KeyRotator\config.json"
```

so the service and the interactive UI share one config file. The service starts at boot,
before any login, and Windows restarts it automatically after a crash.

Prefer not to install a service? Drop a shortcut to `bin\keyrotator.exe` into
`shell:startup` (Win+R → `shell:startup`) to launch it at login instead.

---

## Starting the server

Running `bin\keyrotator.exe` with no command starts the server.

| Flag | Description |
| --- | --- |
| `-config PATH` | config file (default `%APPDATA%\KeyRotator\config.json`) |
| `-listen ADDR` | override listen address (default `127.0.0.1:8787`) |
| `-open` | open the dashboard in your browser on start |
| `-service CMD` | `install` / `uninstall` / `start` / `stop` / `restart` / `status` / `run` |
| `-service-run` | used by the SCM; you don't call this directly |
| `-version` | print version |

## Managing a running instance from the terminal

`keyrotator <command>` is a thin client over the running server's admin API, so every change lands
on the live process immediately (no restart) and is persisted to the config. It finds the server —
and the admin token — in the same config file, so there is no extra setup:

```powershell
.\bin\keyrotator.exe list                                 # providers, key counts, limits
.\bin\keyrotator.exe show OpenAI                          # fields, keys, rates, token
.\bin\keyrotator.exe add OpenAI https://api.openai.com/v1 --key-rps 10
.\bin\keyrotator.exe add-key OpenAI sk-... --label "team A"
.\bin\keyrotator.exe url OpenAI                           # ready-to-use proxy URL
.\bin\keyrotator.exe token gen                            # turn proxy auth on
.\bin\keyrotator.exe test OpenAI                          # probe every key
```

| Command | Purpose |
| --- | --- |
| `list` | providers with key counts, limits and state (also `ls`, `providers`) |
| `show <provider>` | everything about one provider |
| `add <name> <base-url>` | create a provider — `--strategy`, `--health`, `--provider-rps`, `--key-rps`, `--token` |
| `rm <provider>` | delete a provider (also `remove`, `delete`) |
| `on <provider>` / `off <provider>` | enable / disable a provider |
| `url <provider>` | print the authenticated proxy base URL |
| `keys <provider>` | list keys |
| `add-key <provider> <secret>` | add a key — `--label` |
| `rm-key <provider> <key-id>` | delete a key |
| `key-on` / `key-off <provider> <key-id>` | enable / disable a key |
| `test [provider]` | health-probe keys against the upstream |
| `token [show\|gen\|set <value>\|clear]` | manage the global proxy token |
| `settings` | show global settings |

`<provider>` may be an id, a name, or an unambiguous id prefix. Global flags may appear anywhere:
`-config PATH`, `-addr ADDR`, `-admin-token T` (or `KEYROTATOR_ADMIN_TOKEN`), `-json`.

```powershell
.\bin\keyrotator.exe -addr 127.0.0.1:8797 list        # talk to a non-default instance
.\bin\keyrotator.exe list -json                       # machine-readable output
```

## HTTP API

The dashboard is a thin client over a small JSON API.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/state` | providers, keys, settings, addresses |
| GET | `/api/logs` | recent proxied requests |
| POST | `/api/settings` | `{maxRetries, cooldownSec, adminToken, proxyToken, rateLimitRPS, keyRateLimitRPS}` |
| POST | `/api/providers` | create provider (also accepts `proxyToken`, `rateLimitRPS`, `keyRateLimitRPS`) |
| PUT / DELETE | `/api/providers/{id}` | update / delete provider |
| POST | `/api/providers/{id}/test` | health-probe every key |
| POST | `/api/providers/{id}/keys` | add a key |
| PUT / DELETE | `/api/providers/{id}/keys/{kid}` | update / delete a key |
| POST | `/api/providers/{id}/keys/{kid}/test` | probe one key |
| POST | `/api/providers/{id}/keys/{kid}/reset` | clear cooldown/invalid status |

If an **admin token** is set in Settings, every `/api/*` call must send
`X-Admin-Token: <token>`; the dashboard prompts for it and remembers it locally.

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
edited live in the dashboard (**Edit** → *Request fields*) and saved to the config as
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

Any client-supplied value for those headers/params is replaced. Older configs written with the
single `authHeader`/`authPrefix` fields are migrated to an equivalent `inject` rule automatically
on first load, and rules with no `source` default to `key`.

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
- Keys are stored **in plaintext** in the config file. Protect `%APPDATA%\KeyRotator\config.json`
  (it is written with owner-only permissions), and consider a dedicated low-privilege key set.
- If `config.json` is missing, malformed, or written with a UTF-8 BOM, the app still starts: the
  bad file is moved aside to `config.json.corrupt-<timestamp>` and defaults are used. This keeps
  the service from refusing to boot on startup.
- The proxy endpoint is open to anything that can reach the listen address unless you set a
  **proxy token** (see above). Set one before binding to `0.0.0.0`.

## Development

Requires Go 1.22+ (see `go.mod`). From the repo root:

```powershell
go vet ./...                                             # static checks
go test ./...                                            # unit tests
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1   # build bin\keyrotator.exe
powershell -ExecutionPolicy Bypass -File .\scripts\update.ps1  # stop, rebuild, restart
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to send changes and [AGENTS.md](AGENTS.md)
for the conventions used by AI coding agents working in this repo.

## Project layout

```
keyrotator/
  cmd/keyrotator/          the single main package
    main.go                entrypoint + flags
    app.go                 app wiring, HTTP server, request log, health sweeper
    config.go              config model, atomic persistence
    rotator.go             key selection, cooldown/failure classification
    limits.go              token-bucket rate limiter (provider + per key)
    proxy.go               the rotating proxy handler (/p/...) + proxy auth
    api.go                 admin REST API + embedded UI
    cli.go                 `keyrotator <command>` management CLI
    service_windows.go     Windows service install/run
    service_other.go       non-Windows stub
    *_test.go              unit tests
    web/ui.html            dashboard (embedded into the binary)
  scripts/
    build.ps1              build into bin\keyrotator.exe
    update.ps1             stop the running instance, rebuild, restart
  .github/workflows/       CI (vet, test, build on Linux and Windows)
  bin/                     build output (git-ignored)
```
