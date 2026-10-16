# From the terminal

The management CLI and the full-screen terminal UI. Both talk to the running server, so changes apply
immediately. Back to the [README](../README.md).

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
| `add-key <provider> <secret>` | add a key — `--label` sets its alias (default `key N`), `--part name=value` adds a named part, and can be repeated |
| `alias <provider> <key> <new-name>` | rename a key, by id or current alias |
| `rm-key <provider> <key-id>` | delete a key |
| `key-on` / `key-off <provider> <key-id>` | enable / disable a key |
| `test [provider]` | health-probe keys against the upstream |
| `token [show\|gen\|set <value>\|clear]` | manage the global proxy token |
| `settings` | show global settings |
| `tui` | full-screen terminal dashboard (see below) |
| `start` | run in the background; keeps running after the terminal closes |
| `stop` | stop the background server (graceful; saves the config) |
| `status` | show whether it is running, its pid and config file |

`<provider>` may be an id, a name, or an unambiguous id prefix. Global flags may appear anywhere:
`-config PATH`, `-addr ADDR`, `-admin-token T` (or `KEYROTATOR_ADMIN_TOKEN`), `-json`.

```powershell
.\bin\keyrotator.exe -addr 127.0.0.1:8797 list        # talk to a non-default instance
.\bin\keyrotator.exe list -json                       # machine-readable output
```

## Terminal UI

`keyrotator tui` opens a full-screen dashboard against the running instance. It is a client of the
admin API, like the CLI and the web dashboard, so it always shows live state and never edits the config
file directly. It works in any terminal, including over SSH. Example layout (the data is made up):

```
KeyRotator  v1.0.0  127.0.0.1:8787  ● online
────────────────────────────────────────────────────────────────────────
PROVIDERS                      │ KEYS  OpenAI  2/3 healthy
› OpenAI              3 keys   │   LABEL        KEY            STATUS       OK   ERR  LAST USED
  Anthropic           1 keys   │ › billing      sk-******CCC   ok           41     0  12s ago
                               │   key 2        sk-******FFF   cooling 45s  17     3  2m ago
                               │   key 3        sk-******III   disabled      0     0  -

RECENT REQUESTS
22:50:24  200  POST /chat/completions  OpenAI  billing  312ms
```

| Key | Action |
| --- | --- |
| `↑` `↓` / `j` `k` | move within the focused pane |
| `←` `→` / `tab` | switch between providers and keys |
| `t` / `T` | test the selected key / every key of the provider |
| `e` | enable or disable the selected key |
| `P` | enable or disable the selected provider |
| `n` | rename the selected key's alias |
| `a` | add a key (paste it; the alias defaults to `key N`) |
| `d` | delete the selected key (asks for `y`) |
| `r` | clear a key's cooldown and error state |
| `g` | refresh now |
| `q` / `ctrl+c` | quit (the server keeps running) |

Key values are masked everywhere in the TUI. Full values are never printed.

