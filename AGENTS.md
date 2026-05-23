# Repository Guidelines

## Project Structure & Module Organization

`fundpeek` is a Go CLI/TUI module (`github.com/icpd/fundpeek`). The command entrypoint lives in `cmd/fundpeek/main.go`; keep command parsing there and move reusable behavior into focused `internal` packages.

Important internal packages:

- `app`: coordinates auth, source sync, local portfolio cache, optional 基估宝 push, and cache invalidation.
- `config`, `credential`, `cache`: local setup, credential storage, and JSON cache entries.
- `sources/*`: upstream fund-source clients; normalize source data into `model.SyncInput`.
- `merge`: combines normalized accounts and holdings into the local portfolio shape.
- `valuation`: fund quotes, fund stock holdings, stock quotes, and related caching freshness rules.
- `tui`, `authui`: Bubble Tea terminal interfaces.
- `jsonexport`: script-friendly JSON output built from local portfolio data plus refreshed quotes.
- `real`: 基估宝 Supabase client used only for auth, fetching remote config, and explicit `push real`.
- `model`: shared source constants and normalized data types.

Tests are colocated with implementation files as `*_test.go`. The build output is `./fundpeek`; local caches such as `.gocache/`, `.gomodcache/`, `.fundpeek/`, and generated state must stay out of commits.

## Build, Test, and Development Commands

- `make test`: runs `go test ./...` with a repo-local Go cache.
- `make vet`: runs `go vet ./...`.
- `make build`: builds `./fundpeek` from `./cmd/fundpeek`.
- `go install ./cmd/fundpeek`: installs the current checkout to `$GOBIN` or `$GOPATH/bin`; only run this when the user explicitly asks to update the PATH-installed `fundpeek` command.
- `make verify`: runs test, vet, and build; use this before submitting changes.
- `go run ./cmd/fundpeek --help`: prints CLI usage without creating a binary.

Common runtime commands include `./fundpeek status`, `./fundpeek tui`, `./fundpeek json`, `./fundpeek sync [yjb|xb|all]`, `./fundpeek push real`, `./fundpeek auth real|yjb|xb`, and `./fundpeek logout real|yjb|xb`. `make build` only updates the repo-local `./fundpeek`; use `./fundpeek` for normal CLI validation and do not run `go install` just to test changes. Source aliases are accepted by command parsing: `real/r`, `yangjibao/yjb/yj`, `xiaobei/xb/xbyj`, and `all/a` for sync.

## Product and Data Flow Constraints

- `sync` refreshes the local `portfolio_data` snapshot from authenticated sources. TUI and JSON output read this local portfolio; do not make them implicitly depend on 基估宝 remote data.
- `push real` is an explicit user action that merges local portfolio data into 基估宝 remote config. Do not push remote data from `sync`, `tui`, `json`, refresh handlers, or tests unless the command path is explicitly `push real`.
- New source clients should live under `internal/sources/<source>`, use `internal/httpclient`, return clear errors, and normalize through `model.SyncInput` before reaching `merge`.
- Keep cache key semantics stable: `portfolio_data`, `real_data`, `fund_quote/<code>`, `fund_holdings/<code>`, and `stock_quote/<code>`.
- JSON export should remain script-friendly and backwards-compatible: preserve stable field names, keep funds in the output when individual quote refreshes fail, and report per-fund failures in `errors`.
- TUI changes must preserve list/detail navigation and refresh semantics: `Enter`/right opens detail, `Esc`/left/backspace returns or exits, `r` refreshes current page data, and `R` force-refreshes the relevant cached data.

## Coding Style & Naming Conventions

Follow standard Go style: tabs from `gofmt`, short package names, exported identifiers only when they are part of a package boundary, and errors returned instead of logged from library code. Keep command parsing in `cmd/fundpeek`; put reusable behavior in focused `internal` packages. Use source constants from `internal/model` instead of duplicating string literals for fund sources. Use `internal/httpclient` for network clients so timeout, retry, logging, and safe response-body redaction stay consistent.

## Testing Guidelines

Use Go's built-in `testing` package. Add or update colocated `*_test.go` files for behavior changes, especially command parsing, config loading, cache freshness, merging, valuation parsing, source clients, JSON export, and TUI model updates. Prefer table-driven tests for input normalization and data transforms.

When touching TUI behavior, cover Bubble Tea model transitions, list/detail navigation, refresh and force-refresh cache effects, stale/error states, and fixed-width rendering. When touching sync/push/cache behavior, cover local `portfolio_data` vs `real_data` boundaries and partial-source failure behavior. Run `make test` for focused work and `make verify` before handing off.

## Agent Workflow Expectations

Before editing, inspect the relevant files and preserve unrelated work in the tree. Keep changes scoped to the user-visible behavior requested. After editing, run the narrowest meaningful test command, then broader checks when the change affects shared behavior. Review the final diff for regressions, risky network/logging behavior, accidental credential exposure, and unrelated churn.

## Commit & Pull Request Guidelines

Recent history uses short, imperative commit subjects such as `Add fund valuation TUI` and `Rename project to fundpeek`. Keep commits focused and describe the user-visible change. Pull requests should include a concise summary, tests run (`make verify` output is preferred), linked issues when applicable, and terminal screenshots or recordings for visible TUI changes.

## Security & Configuration Tips

Do not commit credentials, tokens, cache files, generated local state, real portfolio snapshots, or `fundpeek json` output containing personal holdings. Auth flows may read email, phone, OTP, or SMS codes; keep those in local prompts only. Treat source-client and valuation-client changes as network-facing: use timeouts, return clear errors, and avoid printing sensitive response data.
