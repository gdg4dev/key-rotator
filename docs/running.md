# Running KeyRotator

How to keep KeyRotator running: in the background, at login, as a Windows service, or in Docker.
Back to the [README](../README.md).

## Platform support

| | Windows | macOS | Linux |
| --- | --- | --- | --- |
| Proxy, dashboard, CLI | ✅ | ✅ | ✅ |
| Terminal UI (`tui`) | ✅ | ✅ | ✅ |
| Start at login (Settings toggle) | Run key (no admin needed) | LaunchAgent | XDG autostart |
| Windows service (`-service`) | ✅ | — (use launchd, see below) | — (use systemd, see below) |
| Config location | `%APPDATA%\KeyRotator` | `~/Library/Application Support/KeyRotator` | `$XDG_CONFIG_HOME/KeyRotator` (default `~/.config`) |
| Build | `scripts\build.ps1` | `scripts/build.sh` | `scripts/build.sh` |
| Tested | Windows 11 | CI only (cross-compiled and vetted) | Ubuntu on WSL2 |

macOS is exercised by the CI matrix. Start-at-login on macOS has not been run on real hardware yet, so
please report any problems.

## Use keyrotator from any terminal

```sh
keyrotator path on      # first time: ./bin/keyrotator path on, or .\bin\keyrotator.exe on Windows
keyrotator path         # is it on?
keyrotator path off     # undo
```

The same switch is in **Settings → Use from any terminal**.

| OS | What it does |
| --- | --- |
| Windows | Adds the folder holding `keyrotator.exe` to your **user** PATH (no Administrator rights needed) and tells Windows the environment changed. |
| macOS, Linux | Creates a link `~/.local/bin/keyrotator`. If `~/.local/bin` is not on your PATH yet (the default on macOS), add `export PATH="$HOME/.local/bin:$PATH"` to `~/.zshrc` or `~/.bashrc`; KeyRotator tells you when that is needed. |

Open a new terminal afterwards: terminals that are already open keep their old PATH. If you move the
KeyRotator folder, run `path on` again from the new place.

## Run in the background

A server started in a terminal stops when that terminal closes. To keep it running:

```powershell
.\bin\keyrotator.exe start     # Windows
.\bin\keyrotator.exe status
.\bin\keyrotator.exe stop
```

```sh
./bin/keyrotator start        # macOS and Linux
```

`start` launches a detached process. Its log is `keyrotator.log` and its pid is `keyrotator.pid`, both next to
the config file. For a server that starts at login, use the **Start at login** switch in Settings.

## Start at login and background services

The **Start at login** switch in Settings (or `POST /api/autostart` with `{"enabled": true}`) registers
KeyRotator with the operating system. Nothing is stored in the config file, so the switch always
reflects what the OS will actually run.

| OS | Mechanism | Where |
| --- | --- | --- |
| Windows | per-user Run key (no Administrator rights needed) | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` |
| macOS | LaunchAgent, started at login and restarted after a crash | `~/Library/LaunchAgents/com.keyrotator.agent.plist` |
| Linux | XDG autostart entry for desktop sessions | `~/.config/autostart/keyrotator.desktop` |

The login instance is started with `-quiet -config <your config file>`, so it reads the same state you
configured.

On a headless Linux server, use a systemd user unit instead:

```ini
# ~/.config/systemd/user/keyrotator.service
[Unit]
Description=KeyRotator API key proxy
After=network-online.target

[Service]
ExecStart=%h/keyrotator/bin/keyrotator -quiet
Restart=on-failure

[Install]
WantedBy=default.target
```

```sh
systemctl --user enable --now keyrotator
loginctl enable-linger "$USER"     # keep it running when nobody is logged in
```

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

## Starting the server

Running `bin\keyrotator.exe` with no command starts the server.

| Flag | Description |
| --- | --- |
| `-config PATH` | config file (default: the OS user config directory, see [Platform support](#platform-support)) |
| `-listen ADDR` | override listen address (default `127.0.0.1:8787`) |
| `-open` | open the dashboard in your browser on start |
| `-service CMD` | `install` / `uninstall` / `start` / `stop` / `restart` / `status` / `run` |
| `-service-run` | used by the SCM; you don't call this directly |
| `-version` | print version |

## Docker

A minimal image (distroless, non-root, read-only root filesystem). The config lives on a volume, so
upgrading the image keeps your keys and settings.

```sh
docker compose up -d                          # builds the image and binds 127.0.0.1:8787
docker compose exec keyrotator keyrotator list
```

The compose file publishes the port on `127.0.0.1` only. Before exposing it to any other network,
set a proxy token and an admin token in Settings.

