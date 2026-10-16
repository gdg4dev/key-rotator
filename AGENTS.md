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
| `cmd/keyrotator/app.go` | Wiring, HTTP server, request log, health sweeper |
| `cmd/keyrotator/config.go` | Config model, atomic persistence, migration |
| `cmd/keyrotator/rotator.go` | Key selection, header/query injection, failure classification |
| `cmd/keyrotator/limits.go` | Token-bucket rate limiter (per provider and per key) |
| `cmd/keyrotator/proxy.go` | The `/p/...` proxy handler and proxy-token auth |
| `cmd/keyrotator/api.go` | Admin REST API; embeds `web/ui.html` |
| `cmd/keyrotator/cli.go` | `keyrotator <command>` management CLI |
| `cmd/keyrotator/service_windows.go` | Windows service install/run (`//go:build windows`) |
| `cmd/keyrotator/service_other.go` | Non-Windows stub (`//go:build !windows`) |
| `cmd/keyrotator/web/ui.html` | Dashboard (single file, embedded into the binary) |
| `scripts/build.ps1` | Builds `bin\keyrotator.exe` |
| `scripts/update.ps1` | Stops the running instance, rebuilds, restarts it |

Config lives at `%APPDATA%\KeyRotator\config.json` by default (override with
`-config`). It is never part of the repo.

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

- **Standard library only.** Do not add third-party modules without asking.
- **Keep it one binary.** New UI assets go under `cmd/keyrotator/web/` and are
  embedded; do not read them from disk at runtime.
- **Never expose raw keys.** Use `Key.Masked()` for anything displayed or logged.
- **Platform code stays behind build tags.** Anything touching the Windows
  service API goes in `service_windows.go`, with a stub in `service_other.go`.
- **Concurrency.** Provider state is guarded by `Provider.mu`; `Store` methods
  take their own locks. Hold locks briefly and never call out to upstreams while
  holding one.
- **Config changes must persist atomically** through `Store.Save`, and a live
  edit must take effect on the next request without a restart.
- **Tests.** Logic changes need a unit test in `cmd/keyrotator/*_test.go`. Keep
  tests offline: no real upstream calls and no real API keys.
- **README stays accurate.** If you change a flag, endpoint, or file path, update
  `README.md` in the same change.

## Things to ask before doing

- Running `scripts/update.ps1` or `bin\keyrotator.exe` stops or starts a live
  instance on `127.0.0.1:8787`. Ask first if the user may be using it.
- Do not read, print, or commit a real `config.json` or any API key.
- Do not change the default listen address, the `/p/` URL shape, or the config
  file format without a migration path. Existing users depend on them.

## Git

- Commit messages: short imperative subject, body when the reason is not obvious.
- Do not commit `bin/`, `*.exe`, or `config.json` (already in `.gitignore`).
