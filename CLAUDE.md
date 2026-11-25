# lazyobs

Terminal UI (Go, Charm Bubble Tea v2 / Bubbles v2 / Lip Gloss v2) for the Centreon
observability platform (formerly Centreon Log Management). Spirit: lazygit / k9s.
Logs are the first signal; traces and metrics must plug in without refactoring.

- Module: `github.com/CentreonLabs/lazyobs` (repository not created yet; work stays local until it is)
- License: Apache-2.0
- Go: version pinned in `go.mod` (toolchain auto-download)

## Commands

```sh
make build    # ./bin/lazyobs with version ldflags
make test     # go test -race ./...
make lint     # golangci-lint v2 via `go run` (pinned in Makefile)
make integration  # live API tests, needs LAZYOBS_TOKEN + LAZYOBS_ORG
make fmt      # gofumpt + goimports through golangci-lint
make cross    # build all OS/arch into ./dist
```

## Architecture

Dependency direction: `ui → app → engine → obs ← source → api`. `internal/obs` imports nothing from the project.

| Package                  | Role                                                                                                                                         |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `cmd/lazyobs`            | Entry point, cobra subcommands, TUI launch                                                                                                   |
| `internal/obs`           | Signal-agnostic domain: `Query`, `TimeRange`, `Cursor`, `Page[T]`, `Series`, `Searcher[T]`, `Histogrammer`                                   |
| `internal/obs/logs`      | Log domain: `Record`, `Severity`, dedup fingerprint                                                                                          |
| `internal/api`           | Hand-written HTTP client (DTOs, errors, auth); fixtures in `testdata/`                                                                       |
| `internal/source/remote` | Adapts `api` to `obs` interfaces                                                                                                             |
| `internal/source/fake`   | Synthetic data source (tests, `--demo`)                                                                                                      |
| `internal/engine`        | UI-free logic: pager (offset → time-window), live tail (poll + dedup)                                                                        |
| `internal/config`        | Contexts, env/flag overrides, `Secret` type                                                                                                  |
| `internal/app`           | Root Bubble Tea model, signal registry (`SignalView`)                                                                                        |
| `internal/session`       | Saved sessions (tabs per context) in `$XDG_STATE_HOME/lazyobs/state.json`; tab contents are opaque JSON owned by the view (`logsview.State`) |
| `internal/ui`            | Theme, keymaps, `uitest` headless driver, per-signal views (`ui/views/logsview`)                                                             |

Adding a signal = domain type in `obs/<signal>`, mapping in `api/`, `remote` searcher, view in `ui/views/<signal>`, register it in `app`.

UI conventions:

- Views implement `app.SignalView`; the root forwards key presses to the active view only, other messages (async results, ticks, `app.ResizeMsg`) to every view.
- Async results carry the ID of the view that asked plus the search ID / live generation they belong to; results of other views and stale ones are dropped. Several instances of a view coexist in tabs (`internal/ui/tabset`).
- Report errors and warnings with `app.Notify` (status bar), never by printing.
- UI tests drive the root model with `internal/ui/uitest` (send keys, run commands, read the plain-text screen). Set `LAZYOBS_SCREENS=1` to print screens in `internal/app` tests. Inject `Options.Now` and `Options.After` to control time and live polling.
- `lazyobs --demo` runs on `source/fake`: use it to try UI changes without an account.

## API facts (verified against the live API, not all in the spec)

- Spec: https://docs-api.centreon.com/api/centreon-log-management/openapi.json
- Base URL: `https://api.euwest1.obs.mycentreon.com`; only region for now.
- Auth: header `X-Api-Key` (M2M token). Org in path: `/v1/orgs/{org}/...`, pattern `^[a-zA-Z0-9-_]{1,50}$`.
- A wrong organization code also returns **401** (not 403/404).
- M2M tokens get **401** on `/v1/orgs`, `/v1/orgs/{org}`, `/bookmarks`. Validate a profile with a `max_hits=0` search instead.
- `GET|POST /v1/orgs/{org}/logs/search`: Quickwit query syntax; `start`/`end` are Unix **seconds** (negative `start` is relative to `end`); `max_hits` ≤ 1000 (default 20); `start_offset` ≤ **10 000**.
- Response: `{hits: [...], num_hits, elapsed_time_micros, errors: []}`. The spec types `hits` as a bare object; it is an array of flat OTel-like records: `timestamp_nanos`, `observed_timestamp_nanos`, `severity_text`, `severity_number`, `service_name`, `scope_name`, `scope_version`, `trace_flags`, `body` (object, e.g. `{message}`), `resource_attributes` (object). Treat other fields (`attributes`, `trace_id`, `span_id`) as optional.
- Hits are sorted newest first; there is no sort parameter (`sort_by` is ignored) and **no unique document ID**.
- POST with `aggregation: {aggregationType: count|severity|service_name|{custom:{field}}, targetResolution, interval}` adds `curves[]`; curve `times` are **milliseconds**.
- `/logs/fields/capabilities` and `/logs/fields/{field}/terms` work (autocomplete, facets). Only fields listed by capabilities can be searched: `scope_name`/`scope_version` are stored but give "not full-text searchable", `trace_flags` a 500. Terms only work on `aggregatable` fields (`body.message` gives 400).
- Fields with a `text` type have no word positions: a quoted multi-word value fails ("phrase query on field which does not have positions indexed"); search them word by word (`(f:a AND f:b)`).
- Errors: `{"message": "..."}` with 400/401/402/403/413/422. The platform **rate-limits**: `429 {"message":"rate limit exceeded, retry after 1s"}`; the client retries up to 3 times after the delay (Retry-After header or message).
- No streaming endpoint: live mode polls every **10 s** with an overlap window and dedups on a fingerprint.
- `POST /log-analysis/nlquery/translate` (**Editor** role) takes `{prompt, available_services?}` and returns `{query, time_range, warning}`; `time_range` is a look-back in **milliseconds**, `{start, end}` in epoch ms, or null. We ground it with the top `service_name` terms of the last 7 days.
- Traces (beta, enabled per org, 404 otherwise). `POST /traces/search` matches **root spans only** (one hit per trace; inner services are not searchable), hits are spans: `trace_id`, `span_id`, `parent_span_id`, `service_name`, `span_name`, `span_kind` (OTel int), `span_start/end_timestamp_nanos`, `span_duration_millis`, `span_status {code: ok|error|unset, message}`, `span_attributes`, `resource_attributes`. **`sort_direction` is reversed** (desc returns ascending, the default is oldest first): `remote.Traces` detects it from the results and flips. Aggregation needs `interval` and only counts traces (curve `attributes` is an array, not the documented object). `GET /traces/{id}` returns `{traceId, rootSpanId, rootSpanStatus, services, traceName, totalDuration (ms, root only), spans}` with spans unordered; async children may end after the root. `/traces/topology` gives per-service p50/p95/p99, errors and call edges. Synthetic traces (`lazyobs-demo-*`) were ingested once into mbe-org on 2026-11-25 through `https://otlp.euwest1.obs.mycentreon.com/v1/ingress/otlp/v1/traces` (OTLP JSON, org from the token).

## Conventions

- All code, comments, docs and commits in English; inclusive terminology.
- **Never print, log or embed the token.** It lives in `config.Secret` whose `String`/`GoString`/`LogValue`/`MarshalJSON` redact it; only the HTTP transport reads the raw value. Tests assert the token never leaks into errors or output.
- The API client must stay mockable: consumers depend on `obs` interfaces, never on `*api.Client` directly. Client tests use `httptest` with anonymized fixtures.
- No logging to stdout/stderr while the TUI runs; use `log/slog` to a file behind `--debug`.
- Inject clocks (`func() time.Time`) in time-dependent code so the engine is testable.
- Small, tested steps; `make lint test` must pass before committing.
- Git: `CentreonLabs` is a Centreon product org → once the remote exists, use feature branches and PRs, never push to `main`.
