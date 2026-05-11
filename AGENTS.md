# Repository Guidelines

## Project Structure & Module Organization

`fundpeek` is a Go CLI/TUI module (`github.com/icpd/fundpeek`). The command entrypoint lives in `cmd/fundpeek/main.go`. Internal packages are under `internal/`: `app` coordinates workflows, `config` and `credential` handle local setup, `backup` writes restore points, `cache` stores local JSON cache entries, `real` talks to the 基估宝 Supabase backend, `sources/*` contains upstream fund-source clients, `valuation` fetches fund quotes, stock holdings, and stock quotes, `merge` combines records, and `tui`/`authui` provide terminal interfaces. Tests are colocated with implementation files as `*_test.go`. The build output is `./fundpeek`; local caches such as `.gocache/` are ignored.

## Build, Test, and Development Commands

- `make test`: runs `go test ./...` with a repo-local Go cache.
- `make vet`: runs `go vet ./...`.
- `make build`: builds `./fundpeek` from `./cmd/fundpeek`.
- `make verify`: runs test, vet, and build; use this before submitting changes.
- `go run ./cmd/fundpeek --help`: prints CLI usage without creating a binary.

Common runtime commands include `./fundpeek status`, `./fundpeek tui`, `./fundpeek sync all`, and `./fundpeek auth real|yjb|xb`. Source aliases are accepted by command parsing: `real/r`, `yangjibao/yjb/yj`, `xiaobei/xb/xbyj`, and `all/a` for sync.

## Coding Style & Naming Conventions

Follow standard Go style: tabs from `gofmt`, short package names, exported identifiers only when they are part of a package boundary, and errors returned instead of logged from library code. Keep command parsing in `cmd/fundpeek`; put reusable behavior in focused `internal` packages. Use source constants from `internal/model` instead of duplicating string literals for fund sources. Use `internal/httpclient` for network clients so timeout, retry, logging, and safe response-body redaction stay consistent.

## Testing Guidelines

Use Go's built-in `testing` package. Add or update colocated `*_test.go` files for behavior changes, especially command parsing, config loading, cache freshness, merging, valuation parsing, source clients, and TUI model updates. Prefer table-driven tests for input normalization and data transforms. Run `make test` for focused work and `make verify` before handing off.

## Commit & Pull Request Guidelines

Recent history uses short, imperative commit subjects such as `Add fund valuation TUI` and `Rename project to fundpeek`. Keep commits focused and describe the user-visible change. Pull requests should include a concise summary, tests run (`make verify` output is preferred), linked issues when applicable, and terminal screenshots or recordings for visible TUI changes.

## Security & Configuration Tips

Do not commit credentials, tokens, backups, cache files, or generated local state. Auth flows may read email, phone, OTP, or SMS codes; keep those in local prompts only. Treat source-client and valuation-client changes as network-facing: use timeouts, return clear errors, and avoid printing sensitive response data.
