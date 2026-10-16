# Contributing

Thanks for taking the time to improve keyrotator. Bug reports, fixes, and
feature ideas are all welcome.

## Ground rules

- **Standard library only.** The project has no third-party dependencies and
  should stay that way unless there is a very strong reason.
- **One binary, no runtime.** Anything new must still compile into the single
  `keyrotator.exe` (the UI is embedded with `go:embed`).
- **Never log or return raw API keys.** Use the masked form for anything shown
  in the UI, logs, or CLI output.
- **Windows-specific code** lives in `*_windows.go` files behind build tags, with
  a non-Windows counterpart, so the project keeps building on Linux and macOS.

## Getting set up

1. Install Go (1.22 or newer; see `go.mod`).
2. Clone the repo and run the checks:

   ```sh
   go vet ./...
   go test ./...
   gofmt -l .        # should print nothing
   ```

3. Build locally with `scripts\build.ps1` on Windows, or `go build ./cmd/keyrotator`
   elsewhere.

## Making a change

1. Open an issue first for anything larger than a small bug fix, so we can agree
   on the approach.
2. Create a branch from `main`.
3. Keep the change focused. Add or update tests in `cmd/keyrotator/*_test.go`
   for logic changes (selection, injection, rate limiting, config handling).
4. Make sure `go vet`, `go test`, and `gofmt` are clean. CI runs the same checks.
5. Update `README.md` if you change behaviour, flags, endpoints, or file layout.
6. Open a pull request describing **what** changed and **why**.

## Commit messages

Short, imperative subject line (e.g. `Add per-key rate limit to CLI test`), with
a body when the reason is not obvious from the diff.

## Reporting security issues

Please do not open a public issue for security problems. See
[SECURITY.md](SECURITY.md).
