# Contributing

Thanks for taking the time to improve keyrotator. Bug reports, fixes, and
feature ideas are all welcome.

## Ground rules

- **Keep dependencies minimal.** The server uses only the standard library. The
  terminal UI adds Bubble Tea and Lip Gloss. Any new module needs a clear reason,
  and it should be mentioned in the pull request description.
- **One binary, no runtime.** Anything new must still compile into the single
  `keyrotator.exe` (the UI is embedded with `go:embed`).
- **Never log or return raw API keys.** Use the masked form for anything shown
  in the UI, logs, or CLI output.
- **OS-specific code lives in `platform_<os>.go`.** See "Supporting every platform" below.

## Getting set up

1. Install Go (1.24 or newer; see `go.mod`).
2. Clone the repo and run the checks:

   ```sh
   go vet ./...
   go test ./...
   gofmt -l .        # should print nothing
   ```

3. Build locally with `scripts\build.ps1` on Windows or `scripts/build.sh` on macOS and
   Linux. Both write to `bin/`.

## Supporting every platform

KeyRotator runs on Windows, macOS, Linux and the BSDs. Everything that differs between them lives in
one file per OS, and each file implements the same few hooks, listed at the top of
`cmd/keyrotator/platform.go`:

| File | Used on |
| --- | --- |
| `platform_windows.go` | Windows |
| `platform_darwin.go` | macOS |
| `platform_linux.go` | Linux |
| `platform_other.go` | other Unix systems (FreeBSD, OpenBSD, ...) |
| `platform_unix.go` | every Unix system, including macOS and Linux |

To add OS-specific behaviour, add a hook to the list in `platform.go` and implement it in each file.
Never branch on `runtime.GOOS` elsewhere. Before opening a pull request that touches platform code,
check that every target still builds:

```sh
for os in windows linux darwin freebsd; do GOOS=$os go vet ./...; done
```

## Finding your way around

```
cmd/keyrotator/
  main.go         entrypoint and flags; `keyrotator <command>` goes to cli.go
  app.go          wiring, HTTP server, request log, health checks, shutdown
  proxy.go        the /p/... proxy: auth, rate limits, retries and failover
  rotator.go      choosing a key within a key group, writing it into the request, bundles
  pool.go         key groups: one key from each group a request needs, pinning
  parts.go        key parts (matched pairs such as key + session id)
  limits.go       token-bucket rate limiter
  config.go       config model, loading, migration, atomic saves
  alias.go        key aliases ("key 1", "billing") and their uniqueness
  activity.go     saves and restores the Activity log (activity.json)
  pathlist.go     PATH list editing for `keyrotator path`
  api.go          the admin JSON API; serves the embedded dashboard
  web/ui.html     the dashboard, a single file embedded into the binary
  cli.go          the management CLI
  tui.go          the terminal UI, a client of the admin API
  daemon.go       start / stop / status for background mode
  metrics.go      Prometheus endpoint
  platform*.go    OS-specific hooks (see above)
  service_*.go    Windows service, with a stub elsewhere
  *_test.go       tests
docs/             user guides linked from the README
scripts/          build and update scripts for each OS
```

## Making a change

1. Open an issue first for anything larger than a small bug fix, so we can agree
   on the approach.
2. Create a branch from `main`.
3. Keep the change focused. Add or update tests in `cmd/keyrotator/*_test.go`
   for logic changes (selection, injection, rate limiting, config handling).
4. Make sure `go vet`, `go test`, and `gofmt` are clean. CI runs the same checks.
5. Update `README.md` or the guide in `docs/` if you change behaviour, flags, endpoints, or file layout.
6. Open a pull request describing **what** changed and **why**.

## Commit messages

Short, imperative subject line (e.g. `Add per-key rate limit to CLI test`), with
a body when the reason is not obvious from the diff.

## Reporting security issues

Please do not open a public issue for security problems. See
[SECURITY.md](SECURITY.md).
