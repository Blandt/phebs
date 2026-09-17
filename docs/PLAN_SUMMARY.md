# phebs plan summary

> Orientation digest for [PLAN.md](../PLAN.md), the project's append-only
> architecture and decision ledger (~2 MB). Authority lives in PLAN.md's
> dated entries and in [BACKLOG.md](./BACKLOG.md)'s acceptance criteria;
> this page only summarizes. Linked from the
> [documentation map](./README.md).

## What phebs is

Independent self-hosted code search in one Go binary ("febz"). zoekt serves
as a library (`query.Parse`, `shards.DirectorySearcher`); index builds run in
child binaries compiled from the same `go.mod` SHA (OOM isolation). State
and job queues live in a supervised local SurrealDB 3.0 child
(`surrealkv://`, WS SDK — no embedded engine); queues use jittered polling,
no Redis. The API is huma v2 (OpenAPI free); the UI is Vite + React +
CodeMirror 6, embedded in the binary via `go:embed`. `git` is exec'd for
clone/fetch into bare repos. Apache-2.0, single-tenant, HEAD is the
authoritative revision. Personal project, personal hardware.

## Key architecture decisions

- **2026-07-07 — zoekt as a library.** A ~200-line Go spike proved serving
  over existing zoekt shards via library import (P0).
- **2026-07-09 — SurrealDB 3.0 as supervised child.** No embedded Go engine;
  jittered-polling job queues instead of Redis/BullMQ; server mode exists
  only in the P6 fleet profile.
- **2026-07-11 — product layer.** React SPA with local/session/API-key auth
  and OIDC; stateless MCP server; committed SCIP code navigation and Git
  history. Built-in chat stays optional.
- **2026-07-22/27 — experimental annex, default-dark.** Contract
  intelligence (Epics 11–15), Caller Map (Epic 20), Change Workbench
  (Epic 21) are implemented but off by default; external validation
  `NOT_ESTABLISHED` — no numeric accuracy or completeness claims exist.
- **2026-08-02 — service scope (Epic 30).** Strict analysis-unit contract,
  focused physical indexing, exact focused evidence publication.
- **2026-08 — microservice v2 (Epics 32–39).** Canonical service-catalog
  contract, immutable repository source/search generations, authorized
  HTTP/MCP readers, namespace-sharded declaration/resolver/caller catalogs.
  Logical services become independent catalog, query, evidence, and workflow
  scopes over shared repository generations.
- **2026-08-30 — scale convergence (Epic 40, closed).** Bounded
  derived-pipeline convergence for ≥2 M regular-file owners; the frozen
  T40.13 ceremony's mechanics gate passed, but **no release, scale, or SLO
  claim is established** (`DO_NOT_RELEASE` posture).
- **2026-09 — service scale (Epic 41, closed).** 10,000 accepted logical
  services proven against the v3 profile; production cap unchanged at 4,000.
- **2026-09 — combined scale (Epic 42, in progress).** T42.1 freeze contract
  integrated (canonical `spike/t421/plan.json`); T42.2 runner implementation
  underway under the task-scoped freeze delegation, which delegates
  reviewed prerequisite integration/push and V3 author/seal once their
  existing gates pass. Ceremony execution, unrelated work, and release or
  scale claims each still need Ben's explicit authorization.
- **2026-08-08 — presentation track (Epic 43, complete).** Runs in parallel
  under [DESIGN_CHARTER.md](./DESIGN_CHARTER.md); owns `ui/` and may not
  touch a scale plane, authority, or claim.

## Current posture (2026-09-16)

- Single-node core complete; experimental stacks remain default-dark.
- Active sequencing: [ROADMAP.md](./ROADMAP.md). Active tickets:
  [BACKLOG.md](./BACKLOG.md). Completed history:
  [BACKLOG_COMPLETED.md](./BACKLOG_COMPLETED.md).
- Merge rule: **agents never merge into `main` without Ben's explicit
  request.**
- `v0.2.0` is an immutable but unverified historical tag; `v0.2.1-dev` is
  the current source line, tagged only after its exact main commit passes
  the hosted gate. P6 fleet work remains demand-driven.
