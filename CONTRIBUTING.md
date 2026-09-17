# Contributing to phebs

phebs is an independent, self-hosted code-search tool in one Go binary
(pronounced "febz"), maintained by Ben as a personal project. This page covers
the contributor workflow; product context lives in
[docs/PLAN_SUMMARY.md](docs/PLAN_SUMMARY.md), and agent working rules live in
[AGENTS.md](AGENTS.md).

## Build and test

Toolchains are pinned: `.go-version`, `.node-version`,
`.golangci-lint-version`, `.surrealdb-version`.

| Command | What it does |
|---|---|
| `make dev` | Boot phebs with the embedded UI (`ARGS="-config phebs.yaml"` for flags) |
| `make dev-api` | Backend-only loop, no UI build |
| `make build` | Version-stamped binary with embedded UI |
| `make test` | Full Go suite (`go test ./... -timeout=60m`) |
| `make ui-test` | Vitest UI tests |
| `make lint` | `golangci-lint run` (must be clean) |
| `make docs-check` | Tracked doc links resolve; the docs map covers every `docs/*.md` |
| `make ci` | The hosted static + Go + race + UI gates |

If you add a doc under `docs/`, link it from
[docs/README.md](docs/README.md) so `make docs-check` keeps passing.

## Branches and tickets

- Work is ticketed in [docs/BACKLOG.md](docs/BACKLOG.md) and proceeds in
  ticket order. Branch names carry the ticket ID, e.g. `t1.3-job-claim-spike`.
- Changes are PR-sized and stacked; one ticket per PR. `main` is the
  integration branch; ticket worktrees and branches are temporary — remove
  them after a verified fast-forward merge.
- Completed tickets move to
  [docs/BACKLOG_COMPLETED.md](docs/BACKLOG_COMPLETED.md).

## The PR bar

- The acceptance criteria (ACs) in `docs/BACKLOG.md` are the merge bar.
- Passing the bar authorizes a merge *request*, not the merge itself:
  **agents never merge into `main` without Ben's explicit request.**
- Style: table-driven tests, `context.Context` first parameter, errors
  wrapped with `%w` and classified at boundaries. Every epic ends demoable
  via `make dev` — an epic that can't be shown end-to-end is not done.
- Every implementation review includes a steady-state-cost pass: enumerate
  per-query/request, per-sync-tick, and startup work; identify held locks,
  repeated full-corpus reads, and worst-case memory/disk/child-process cost.

## Where decisions get recorded

- Every decision lands in [PLAN.md](PLAN.md) as a dated ADR bullet
  **in the same PR** as the change. PLAN.md is append-only — never rewrite
  historical entries.
- Behavior changes update the owning task guide under `docs/guides/`
  (indexed by [docs/MANUAL.md](docs/MANUAL.md)) in the same PR.
- New here? Start with [docs/PLAN_SUMMARY.md](docs/PLAN_SUMMARY.md), then
  [docs/ROADMAP.md](docs/ROADMAP.md) for current sequencing.

## Hard rules

- **Independent implementation:** do not open, copy, or paraphrase
  proprietary or source-available application code, UI, CSS, or schemas as
  implementation references. phebs is Apache-2.0.
- Depend directly on upstream `github.com/sourcegraph/zoekt`.
- No employer code, credentials, hosts, or infrastructure.
