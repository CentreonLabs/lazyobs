# lazyobs

A terminal UI for the Centreon observability platform (formerly Centreon Log Management).
In the spirit of lazygit, lazydocker and k9s: search, filter and investigate your
observability data without leaving the terminal.

> **Status:** early development. Logs and traces are supported; metrics will follow.

## Features

- Search logs with the Quickwit query syntax over a relative or absolute time range
- Query autocompletion: field names, then their most frequent values over the displayed period
- Logs around a log: the same service ±30 s in a new tab, the original log selected and marked
- Filter from a log: pick a field in the detail panel and add it to the query (`+`) or exclude it (`-`)
- Top values: the distribution of any field (`b`) next to the timeline, following the query; filter on a value
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
- Tabs: several searches side by side, each with its own query, time range and live mode
- Traces (beta on the platform): trace list sorted by time or duration, volume and errors over
  time, and a waterfall of each trace's spans with their details
- Navigate between signals: a log's trace (`T`), a trace's logs (`l`)
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

| Keys                                            | Action                                                                           |
| ----------------------------------------------- | -------------------------------------------------------------------------------- |
| `j`/`k`, `g`/`G`, `ctrl+d/u`                    | Move in the list (or the focused panel)                                          |
| `/`                                             | Edit the query                                                                   |
| `enter` or `tab`, `↑`/`↓` while typing a query  | Insert / choose a suggested field or value (`enter` again searches)              |
| `⌥⌫` `⇧⌫` `ctrl+w` / `⌘⌫` `ctrl+u` while typing | Delete the previous word / everything before the cursor                          |
| `a`                                             | Ask in plain words (natural-language query)                                      |
| `t`                                             | Time range (`tab` cycles presets)                                                |
| hover the chart                                 | Tooltip with the values of each level in that bucket                             |
| click on the chart                              | Go to the newest log of that bucket in the list (`r` back to the latest)         |
| drag on the chart / `-`                         | Zoom in on a time span / zoom out                                                |
| `shift+←` / `shift+→` (or `[` / `]`)            | Move the time range back / forward by a quarter of its span                      |
| `enter` / `esc`                                 | Open / close the detail panel                                                    |
| `c`                                             | Logs around the selected one (same service, ±30 s, new tab)                      |
| `tab` then `j`/`k`, `+` / `-`                   | In the detail panel: choose a field, filter on it / exclude it                   |
| `b`                                             | Top values of a field next to the timeline (`tab` to it, then `+` / `-` / `esc`) |
| `tab`                                           | Switch focus between list and detail                                             |
| `L` / `space`                                   | Live mode on/off / pause and resume it                                           |
| `r`                                             | Refresh                                                                          |
| `y`                                             | Copy the selected log as JSON                                                    |
| `z`                                             | Toggle local time / UTC                                                          |
| `v`                                             | Split timeline / compact sparkline                                               |
| click a level in the legend                     | Show / hide that level in the timeline                                           |
| right-click a level                             | Show only that level (again: show all)                                           |
| click a row, wheel                              | Select a log, scroll                                                             |
| `ctrl+t` / `ctrl+w`                             | New tab (from the current search) / close tab                                    |
| `<` / `>`, click a tab                          | Previous / next tab                                                              |
| `T`                                             | Open the trace of the selected log                                               |
| `1` `2` `3`, click a header tab                 | Switch signal (logs, traces; metrics soon)                                       |
| `?`                                             | Help                                                                             |
| `q`                                             | Quit                                                                             |

Natural-language queries use the platform's translation service, which requires the Editor role.
The time span mentioned in the question, if any, replaces the current range.

In the **traces** screen (`2`): `/` searches root spans (the platform matches the root span of each
trace), `s` sorts by start time or duration, `enter` opens the waterfall, where `j`/`k` select a span
and `ctrl+d`/`ctrl+u` scroll its details, `l` shows the logs of the trace and `y` copies the span;
`esc` goes back to the list. Time range, zoom and moves work as for logs.

Live mode polls every 10 seconds and re-queries the last minute to catch logs indexed late.

`⇧⌫`, `⌘⌫` and `⌘←/→` need a terminal with enhanced keyboard reporting (Ghostty, kitty, WezTerm,
iTerm2 with "CSI u"); the other shortcuts work everywhere.

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

## Sessions

lazyobs reopens where you left it: on exit, the tabs of the current context (query, time range,
live mode, hidden levels, detail panel, top values field, display preferences) are saved to `~/.local/state/lazyobs/state.json`
(`$XDG_STATE_HOME`; `%LocalAppData%` on Windows) and restored on the next start. Relative ranges
("last 15m") follow the clock, absolute ones stay where they were. Each context, and the demo, has
its own session.

`lazyobs --fresh` starts a new session instead; `--query` and `--range` do too, with those values.

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
