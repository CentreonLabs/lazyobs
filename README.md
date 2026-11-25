# lazyobs

A terminal UI for the Centreon observability platform (formerly Centreon Log Management).
In the spirit of lazygit, lazydocker and k9s: search, filter and investigate your
observability data without leaving the terminal.

> **Status:** early development. Logs are the first supported signal; traces and
> metrics will follow.

## Features (MVP, in progress)

- Search logs with the Quickwit query syntax over a chosen time range
- Paginated list with a detail panel
- Live mode (like `tail -f`) with pause/resume
- Keyboard-driven navigation with vim/lazygit-style shortcuts and a help screen (`?`)
- Several profiles (contexts), like `kubectl`

## Installation

```sh
go install github.com/CentreonLabs/lazyobs/cmd/lazyobs@latest
```

Homebrew packaging will come later.

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

Environment variables override the active context:

| Variable          | Meaning                 |
| ----------------- | ----------------------- |
| `LAZYOBS_URL`     | API base URL            |
| `LAZYOBS_ORG`     | Organization code       |
| `LAZYOBS_TOKEN`   | API token (`X-Api-Key`) |
| `LAZYOBS_CONTEXT` | Context to use          |

Keep the token file readable only by you (`chmod 600`). lazyobs never prints or logs the token.

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
