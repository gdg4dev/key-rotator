# Security Policy

keyrotator holds upstream API keys and forwards traffic on your behalf, so
security reports are taken seriously.

## Supported versions

Only the latest release receives security fixes.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for a security problem.

Instead, use GitHub's private vulnerability reporting on this repository
(**Security → Report a vulnerability**), or contact the maintainer directly.
Include:

- a description of the issue and its impact,
- steps or a proof of concept to reproduce it,
- the version (`keyrotator -version`) and your OS.

You can expect an acknowledgement within a few days. Once a fix is ready, the
issue will be disclosed in the release notes, with credit if you want it.

## Operational guidance

These are the main things to get right when running keyrotator:

- **Keys are stored in plaintext** in `%APPDATA%\KeyRotator\config.json`. Keep
  that file private and prefer low-privilege keys.
- **Turn on proxy authentication** (a proxy token) before exposing the listen
  address beyond `127.0.0.1`. Without it, anything that can reach the port can
  use your keys.
- Keep the admin UI and API on localhost unless you have put them behind
  something that authenticates the caller.
