# AGENTS.md

Guidance for AI coding agents (and humans who prefer a terse brief) working in
this repository.

## What this project is

keyrotator is a single-binary, local API-key rotation proxy written in Go. It
accepts requests on `/p/<provider>/...`, injects the next healthy pooled key
into the outgoing request, fails over on errors, cools keys down, and serves a
localhost dashboard and a management CLI. It runs as a foreground process or as
a Windows service.

## Layout

| Path | Purpose |
| --- | --- |
| `cmd/keyrotator/main.go` | Entrypoint, flags, mode dispatch |
| `cmd/keyrotator/app.go` | Wiring, HTTP server, request log, health sweeper, shutdown |
| `cmd/keyrotator/proxy.go` | The `/p/...` proxy handler, proxy-token auth, retries |
| `cmd/keyrotator/rotator.go` | Key selection within a key group, injection, bundles, failure classification |
| `cmd/keyrotator/pool.go` | Key groups (`pool` in JSON): which groups a request needs, one key from each, pinning |
| `cmd/keyrotator/parts.go` | Key parts (matched pairs such as key + session id) and name validation |
| `cmd/keyrotator/limits.go` | Token-bucket rate limiter (per provider and per key) |
| `cmd/keyrotator/config.go` | Config model, atomic persistence, migration |
| `cmd/keyrotator/alias.go` | Key aliases and their uniqueness |
| `cmd/keyrotator/activity.go` | Saves the Activity log to `activity.json` and restores it at startup |
| `cmd/keyrotator/pathlist.go` | Adding and removing a folder in a PATH list, for `keyrotator path` |
| `cmd/keyrotator/api.go` | Admin REST API; embeds `web/ui.html` |
| `cmd/keyrotator/cli.go` | `keyrotator <command>` management CLI |
| `cmd/keyrotator/tui.go` | Terminal UI, a client of the admin API |
| `cmd/keyrotator/daemon.go` | `start` / `stop` / `status` background mode |
| `cmd/keyrotator/metrics.go` | Prometheus endpoint |
| `cmd/keyrotator/platform.go` | Lists the per-OS hooks; shared start-at-login types |
| `cmd/keyrotator/platform_<os>.go` | One file per OS implementing the hooks (`windows`, `darwin`, `linux`, `other`, `unix`) |
| `cmd/keyrotator/service_windows.go` | Windows service install/run, with a stub in `service_other.go` |
| `cmd/keyrotator/web/ui.html` | Dashboard (single file, embedded into the binary) |
| `docs/*.md` | User guides linked from the README |
| `scripts/build.*`, `scripts/update.*` | Build, and stop-rebuild-restart, for PowerShell and POSIX shells |

Config lives in the OS user config directory by default: `%APPDATA%\KeyRotator\config.json`
on Windows, `~/Library/Application Support/KeyRotator/config.json` on macOS, and
`$XDG_CONFIG_HOME/KeyRotator/config.json` on Linux. Override with `-config`. It is never part of the repo.

## Commands

```powershell
go vet ./...
go test ./...
gofmt -l .                                   # must print nothing
go build -trimpath -o bin\keyrotator.exe ./cmd/keyrotator
```

Go is usually at `%LOCALAPPDATA%\Programs\GoToolchain\go\bin\go.exe` and may not
be on `PATH`; `scripts/build.ps1` handles that.

## Conventions

- **Minimal dependencies.** The server uses only the standard library. The TUI uses
  Bubble Tea and Lip Gloss. Ask before adding any other module.
- **Cross-platform.** Keep the code building on Windows, macOS and Linux. Check with
  `GOOS=darwin go vet ./...` and `GOOS=linux go vet ./...` after platform-specific changes.
- **Keep it one binary.** New UI assets go under `cmd/keyrotator/web/` and are
  embedded; do not read them from disk at runtime.
- **Never expose raw keys.** Use `Key.Masked()` for anything displayed or logged.
- **Platform code goes in `platform_<os>.go`.** Add a hook to the list in `platform.go` and
  implement it for every OS; never branch on `runtime.GOOS` elsewhere. The Windows service
  stays in `service_windows.go`, with a stub in `service_other.go`.
- **Concurrency.** Provider state is guarded by `Provider.mu`; `Store` methods
  take their own locks. Hold locks briefly and never call out to upstreams while
  holding one.
- **Config changes must persist atomically** through `Store.Save`, and a live
  edit must take effect on the next request without a restart.
- **Tests.** Logic changes need a unit test in `cmd/keyrotator/*_test.go`. Keep
  tests offline: no real upstream calls and no real API keys.
- **Docs stay accurate.** If you change a flag, endpoint, or file path, update `README.md`
  or the matching guide in `docs/` in the same change.

## Things to ask before doing

- Running `scripts/update.ps1` or `bin\keyrotator.exe` stops or starts a live
  instance on `127.0.0.1:8787`. Ask first if the user may be using it.
- Do not read, print, or commit a real `config.json` or any API key.
- Do not change the default listen address, the `/p/` URL shape, or the config
  file format without a migration path. Existing users depend on them.

## Git

- Commit messages: short imperative subject, body when the reason is not obvious.
- Do not commit `bin/`, `*.exe`, or `config.json` (already in `.gitignore`).
