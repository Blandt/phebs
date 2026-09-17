# AGENTS.md — phebs

Independent self-hosted code search in one Go binary. Built with
zoekt in-process, SurrealDB 3.0, huma OpenAPI, Vite + React +
CodeMirror 6 UI embedded via `go:embed`. Pronounced "febz".

## Source of truth (read before working)

- `PLAN.md` — architecture + dated ADR bullets. Every decision lands here as an
  ADR bullet **in the same PR** as the change. No other design docs.
- `docs/BACKLOG.md` — epics + PR-sized tickets. Work proceeds in ticket order;
  branch names carry ticket IDs (e.g. `t1.3-job-claim-spike`).
- `docs/ROADMAP.md` — current posture and sequencing; completed tickets live in
  `docs/BACKLOG_COMPLETED.md`.
- `docs/MANUAL.md` — the user-guide index. Behavior changes update the owning
  task guide under `docs/guides/` in the same PR.
- `docs/README.md` — documentation map; the adoption suite
  (VISION/INVESTIGATIONS/PITCH/PILOT_CHARTER/EVIDENCE_PACK_CARD) lives in
  docs/ and must stay mutually consistent: no doc expands the ask of the
  one above it.
- `CONTRIBUTING.md` — build/test commands, branch and ticket conventions,
  PR bar. `docs/PLAN_SUMMARY.md` — one-page orientation for PLAN.md.

## Stack

Go (latest stable, 1.26 line) · `github.com/sourcegraph/zoekt` as a library for
**serving** (`query.Parse`, `shards.DirectorySearcher`); index **builds** via a
child `zoekt-git-index` compiled from the same go.mod SHA (OOM isolation) ·
SurrealDB 3.0 as a **supervised local child** (`surrealkv://`, official Go SDK
over WS — no embedded Go engine, 2026-07-09 ADR) for state **and** job queues
(jittered polling — no Redis, no BullMQ; server mode only in the P6 fleet
profile) · huma v2 for the API (OpenAPI free) · exec `git` for clone/fetch into
bare repos · Vite + React + TS + CodeMirror 6 in `ui/`, embedded in the binary.

## Layout

`cmd/phebs/` · `internal/{api,auth,codenav,compat,config,extract,gitobj,indexer,mcp,recovery,search,servicecatalog,servicecatalogingest,store,sync}` ·
`ui/` · `docs/` · shards at `$DATA/index`, bare repos at
`$DATA/repos/<host>/<path>.git`

## Conventions

- PR-sized, stacked changes; one ticket per PR; ACs in BACKLOG.md are the merge bar.
- `main` is the integration branch. Ticket worktrees and branches are temporary:
 remove them after a verified fast-forward merge; retain unmerged validation
 lineages until an explicit archival decision.
- **Agents never merge into `main` without Ben's explicit request** (2026-07-22).
 Passing the merge bar authorizes a merge *request*, not the merge itself;
 completed ticket work stays on its ticket branch until Ben says to integrate.
- **Task-scoped freeze delegation (2026-09-05).** Ben explicitly delegates
  decisions and agent orchestration through the next T42.2 ceremony freeze,
  including reviewed prerequisite integration/push and V3 author/seal after
  their gates pass. Follow the owning PLAN orchestration and BACKLOG ACs.
  The lead serializes Git and shared spine edits; independent reviewers may
  not waive evidence. This supersedes routine separate-request holds only for
  this task, not the general merge rule, ceremony execution, unrelated tracks,
  host entitlements, admission bounds, release or scale claims.
- **Parallel tracks (2026-08-07).** The scale track owns Epics 40–42 in this
  checkout on `codex/t4*` branches and owns `internal/`, `cmd/`, `spike/`, the
  store schema, `Makefile`, and `go.mod`. The presentation track owns Epic 43
  in `../phebs-ux` on `ux/t43.*` branches and owns `ui/`,
  `docs/DESIGN_CHARTER.md`, screenshot tooling, and retained design records;
  it is governed by `docs/DESIGN_CHARTER.md` and must not alter a scale plane,
  authority, claim, or caveat wording. Scale work must not modify `ui/` or
  `docs/DESIGN_CHARTER.md` without a flagged handoff. Shared spine docs
  (`PLAN.md`, `docs/BACKLOG.md`,
  `docs/ROADMAP.md`, `AGENTS.md`, `docs/BACKLOG_COMPLETED.md`, and
  `docs/README.md`) are append-only across tracks: edit only the owning epic's
  sections and ADR rows, never reflow the other track's text, and the second
  merger resolves conflicts and reruns `make docs-check` and
  `make verify-glossary`. Stage only explicit paths; never use `git add -A` or
  `git add .`, never stage the other track's files, and leave unfamiliar
  `ui/` or design-document changes uncommitted. Any boundary crossing must be
  identified in the ticket summary and wait for Ben's routing rather than
  crossing silently.
- Table-driven tests. Every epic ends demoable via `make dev` — an epic that
  can't be shown end-to-end is not done.
- Every implementation review includes a steady-state-cost pass: enumerate work
  performed per query/request, sync tick, startup/restart, retry/no-op, and
  publication transition; identify held locks, repeated full-corpus/shard
  reads or hashing, cache invalidation, concurrency bounds, and worst-case
  memory/disk/child-process cost. Green functional gates do not replace this
  pass.
- golangci-lint clean. `context.Context` first param. Errors wrapped with `%w`,
  classified at boundaries (T3.3 taxonomy).
- HEAD is the default and authoritative revision. T10.4 may add at most seven
  explicit branch/tag revisions per repository for `rev:` search; extraction,
  SCIP defaults, coverage, and proof bundles remain HEAD-bound. Single-tenant
  posture; the per-user RepoSet hook in the search pre-pass is
  `Searcher.Visible` (T10.3), enabled only when the config has a `permissions:`
  block.

## Hard rules

- **Independent implementation:** do not open, copy, or paraphrase proprietary
  or source-available application code, UI, CSS, or schemas as implementation
  references. Respect dependency licenses and preserve required notices.
  phebs is Apache-2.0 (confirmed, T0.2).
- Depend directly on upstream `github.com/sourcegraph/zoekt`.
- No employer code, credentials, hosts, or infrastructure. Personal project,
  personal hardware.

## History (condensed)

A dated ticket-by-ticket "Current state" record used to live here; it was
condensed on 2026-09-16 to keep this file scannable. Completed narratives
remain in `docs/BACKLOG_COMPLETED.md` and the dated PLAN.md entries; retained
validation provenance for active work also lives in the active owning ledger,
`docs/BACKLOG.md` (e.g. the T42.2h validation record). This section only
orients.

- **2026-07-07 → 07-11: phases P0–P4.** zoekt-as-library spike; single-node
  core (config, SurrealDB store, sync, same-SHA index-pipeline, search API);
  queue correctness + liveness; React SPA + local/session/API-key auth and
  OIDC; MCP + SCIP + Git-history product layer. Built-in chat stays optional.
- **2026-07-22 → 07-27: phases P7–P8.** Contract-intelligence annex
  (Epics 11–15), Caller Map (Epic 20), Change Workbench (Epic 21) —
  implemented but **default-dark**; external validation `NOT_ESTABLISHED`,
  so no numeric accuracy or completeness claims exist.
- **2026-08: Epics 0–24, 29–39.** Single-node core complete; service-scope
  program (Epic 30: strict analysis-unit contract, focused physical indexing,
  exact focused evidence); microservice v2 contract and validation gate
  (Epics 32–39: canonical service catalog, immutable source/search
  generations, authorized HTTP/MCP readers, namespace-sharded
  declaration/resolver/caller catalogs). Epics 25–28 remain unscheduled.
- **2026-08-08: Epic 43 (presentation track).** Charter-governed and
  complete under `docs/DESIGN_CHARTER.md`; owns `ui/` and must not touch a
  scale plane, authority, or claim. Residue queued as T43R.1–5.
- **2026-08-30: Epic 40 closed.** Bounded derived-pipeline convergence for
  ≥2 M regular-file owners; the frozen T40.13 ceremony's mechanics gate
  passed (neutral-47), but **release, scale, and SLO claims remain
  unestablished** and the `DO_NOT_RELEASE` posture stands.
- **2026-09: Epic 41 closed** at main `d92b6673` — 10,000 accepted logical
  services proven against the v3 profile; the production cap is unchanged at
  4,000 services.
- **Now: Epic 42 combined-scale program.** T42.1 freeze contract integrated
  (`ea9dd555`, canonical `spike/t421/plan.json`); T42.2 runner
  implementation is underway under the task-scoped freeze delegation
  recorded above, which delegates reviewed prerequisite integration/push
  and V3 author/seal once their existing gates pass. Ceremony execution,
  unrelated work, and release or scale claims each still need Ben's
  explicit authorization.
- **Tags:** `v0.2.0` is an immutable but unverified historical tag;
  `v0.2.1-dev` is the current source line, tagged only after its exact main
  commit passes the hosted gate. P6 fleet work remains demand-driven.
