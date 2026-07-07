# CLAUDE.md

Guidance for Claude when working in this repository.

## What this is

`pfwebd` is a small Go daemon that exposes an OpenBSD PF firewall through a
**read-only** web dashboard and a REST API. A companion **Terraform provider**
(`terraform-provider-pfwebd/`) is the only writer. The daemon talks to `pfctl`
and `tcpdump` through `doas`, under a strict command allowlist, running as the
unprivileged user `_pfwebd`.

Core principle: **the web UI only displays; all changes go through the API and
are driven by Terraform.** Don't reintroduce write features in the frontend.

## Layout

```
cmd/pfwebd/          daemon entry point (flags, signal handling, wiring)
internal/pfctl/      doas runner (allowlist) + mock backend + output parsers
internal/logs/       hub fanning the pflog SSE stream to clients
internal/rules/      "pfwebd" anchor manager (apply -> confirm/rollback)
internal/auth/       hashed, scoped API token store (file + SIGHUP reload)
internal/api/        REST + SSE endpoints, WithSecurity middleware
internal/web/static/ embedded frontend (vanilla JS/HTML/CSS, no build step)
examples/            doas.conf, pf.conf, pfwebd-table, rc.d, tokens, deploy script
terraform-provider-pfwebd/  Terraform provider (module github.com/gduale/terraform-provider-pfwebd)
```

Go module is `pfwebd` (imports are `pfwebd/internal/...`).

## Commands

Run from the repo root:

```sh
go build ./...
go test ./...
gofmt -l cmd/ internal/        # must print nothing
go run ./cmd/pfwebd -mock      # local dashboard, no firewall needed
```

Provider (separate module):

```sh
cd terraform-provider-pfwebd && go test ./internal/client/   # client has no heavy deps
```

Cross-compile for the firewall:

```sh
GOOS=openbsd GOARCH=amd64 go build -o pfwebd ./cmd/pfwebd     # use arm64 if needed
```

Always run `go build ./...`, `go test ./...` and `gofmt` before committing.

## Key invariants — keep these in sync

- **doas allowlist:** `internal/pfctl/runner.go` (the `allowlist` map, the table
  helper path, the tcpdump argv, and the anchor argv) MUST match
  `examples/doas.conf` exactly — `doas` matches arguments verbatim.
- **Anchor name:** the PF anchor is `"pfwebd"` (`pfctl.AnchorName`). It appears in
  `runner.go`, `examples/doas.conf` (`-a pfwebd`), `examples/pf.conf`
  (`anchor "pfwebd"`), the mock, the frontend and docs. Change all together.
- **Anchor rule policy:** only `pass`/`block`/`match` lines; `anchor`, `include`,
  `load` are rejected. Anti-lockout: validate (`pfctl -n`) -> apply -> confirm
  within `-confirm-timeout` (default 60s) or automatic rollback.
- **Read-only is permanent:** there is no `-read-only` flag. `WithSecurity`
  rejects every non-GET unless authenticated by a token with the `write` scope.
- **Tokens:** never stored in clear text — SHA-256 hashes in the `-tokens-file`
  (`name scope hash`), reloaded on SIGHUP (`rcctl reload pfwebd`). `PFWEBD_TOKEN`
  is an optional implicit write token. Use `pfwebd -gen-token` / `-hash-token`.
- **Env vars:** `PFWEBD_TOKEN`, `PFWEBD_ENDPOINT`, `PFWEBD_MOCK`.
  Keep the `PFWEBD_` prefix and the single `PFWEBD_TOKEN` name everywhere.
- **Naming:** everything user-facing is `pfwebd`, not `pfweb`. When bulk-renaming,
  beware `pfweb` is a prefix of `pfwebd` (use a negative lookahead to avoid
  `pfwebdd`).

## Conventions

- Comments and code identifiers in **English**.
- Prose docs (README, examples) avoid reintroducing the removed rule editor / UI
  write features — the UI is a dashboard.
- The frontend is dependency-free vanilla JS; don't add a build step.
- Tests live next to code (`*_test.go`); add coverage for new `internal/` logic.

## Git

- **Do not commit on the `main` branch.** On any other branch, committing your
  changes is fine.
- This repo lives on a filesystem where git cannot remove its lock files; if a
  commit fails on a stale `index.lock`/`HEAD.lock`, stop and ask the user.
- `*.tfstate*`, `.terraform/`, `.fuse_hidden*` and the Go tarball are gitignored —
  never commit them.
