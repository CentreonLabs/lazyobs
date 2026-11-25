# lazyobs

A terminal UI for the Centreon observability platform (formerly Centreon Log Management).
In the spirit of lazygit, lazydocker and k9s: search, filter and investigate your
observability data without leaving the terminal.

> **Status:** early development. Logs are the first supported signal; traces and
> metrics will follow.

## Features

- Search logs with the Quickwit query syntax over a relative or absolute time range
- Ask in plain words ("errors from checkout in the last hour"), translated to a query by the
  platform
- Zoom on a time span by dragging on the chart
- Split screen: a Grafana-style chart of the log rate per severity on top (braille lines, grid,
  round axes, legend, selected log marked), the log list below
- Endless list (past the API's 10 000 offset limit) with a detail panel: attributes,
  resource, trace IDs and raw JSON
- Live mode (like `tail -f`) with pause/resume
- Keyboard-driven navigation with vim/lazygit-style shortcuts and a help screen (`?`)
- Several profiles (contexts), like `kubectl`
- A `--demo` mode with synthetic data, no account needed

## Installation

```sh
go install github.com/CentreonLabs/lazyobs/cmd/lazyobs@latest
```

Homebrew packaging will come later.

## Usage

```sh
lazyobs                                          # current context, last 15 minutes
lazyobs --range 24h --query 'severity_text:ERROR'
lazyobs --range '2026-11-03 13:00 .. 2026-11-03 14:00'
lazyobs --context staging
lazyobs --demo                                   # try it without an account
```

| Keys                                 | Action                                                      |
| ------------------------------------ | ----------------------------------------------------------- |
| `j`/`k`, `g`/`G`, `ctrl+d/u`         | Move in the list (or the focused panel)                     |
| `/`                                  | Edit the query                                              |
| `a`                                  | Ask in plain words (natural-language query)                 |
| `t`                                  | Time range (`tab` cycles presets)                           |
| drag on the chart / `-`              | Zoom in on a time span / zoom out                           |
| `shift+←` / `shift+→` (or `[` / `]`) | Move the time range back / forward by a quarter of its span |
| `enter` / `esc`                      | Open / close the detail panel                               |
| `tab`                                | Switch focus between list and detail                        |
| `L` / `space`                        | Live mode on/off / pause and resume it                      |
| `r`                                  | Refresh                                                     |
| `y`                                  | Copy the selected log as JSON                               |
| `z`                                  | Toggle local time / UTC                                     |
| `v`                                  | Split timeline / compact sparkline                          |
| click a level in the legend          | Show / hide that level in the timeline                      |
| right-click a level                  | Show only that level (again: show all)                      |
| click a row, wheel                   | Select a log, scroll                                        |
| `1` `2` `3`                          | Switch signal (logs; traces, metrics soon)                  |
| `?`                                  | Help                                                        |
| `q`                                  | Quit                                                        |

Natural-language queries use the platform's translation service, which requires the Editor role.
The time span mentioned in the question, if any, replaces the current range.

Live mode polls every 10 seconds and re-queries the last minute to catch logs indexed late.

lazyobs captures the mouse, so terminals reserve shift+click for themselves: select text with
shift+drag (option+drag in iTerm2), or copy a log with `y`. Shift/alt/ctrl+click on a level also
shows only that level when the terminal forwards it.

## Configuration

lazyobs reads `~/.config/lazyobs/config.yaml` (or `$XDG_CONFIG_HOME/lazyobs/config.yaml`;
`%AppData%\lazyobs\config.yaml` on Windows):

```yaml
current-context: prod
contexts:
  prod:
    url: https://api.euwest1.obs.mycentreon.com # optional, this is the default
    org: my-org
    token-file: ~/.config/lazyobs/token # or token: / token-command:
```

Set it up from the command line, kubectl-style:

```sh
lazyobs config set-context prod --org my-org --token-file ~/.config/lazyobs/token
lazyobs config set-context staging --org my-org-staging --token-command "op read op://vault/lazyobs/token"
lazyobs config get-contexts
lazyobs config use-context staging
lazyobs config view    # effective settings and where each one comes from
lazyobs config check   # verify the platform is reachable
```

The token is never accepted as a command-line flag, so it cannot end up in your shell history.

Precedence: command-line flags (`--context`, `--org`, `--url`, `--config`), then environment
variables, then the context.

| Variable          | Meaning                 |
| ----------------- | ----------------------- |
| `LAZYOBS_CONFIG`  | Configuration file path |
| `LAZYOBS_CONTEXT` | Context to use          |
| `LAZYOBS_URL`     | API base URL            |
| `LAZYOBS_ORG`     | Organization code       |
| `LAZYOBS_TOKEN`   | API token (`X-Api-Key`) |

Keep the token file readable only by you (`chmod 600`); lazyobs warns otherwise. It never prints
or logs the token.

## Development

Requires Go (the exact toolchain is pinned in `go.mod` and downloaded automatically).

```sh
make build   # build ./bin/lazyobs
make test    # unit tests with the race detector
make lint    # golangci-lint (pinned version, no install needed)
make help    # list all targets
```

## License

[Apache License 2.0](LICENSE)
