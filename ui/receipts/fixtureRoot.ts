// Canonical receipt fixture root.
//
// Screenshot receipts must render byte-identical repository identities on
// every machine: the neutral-demo repository names reach the pixels
// unmasked, and the committed baselines pin them. Deriving those names from
// the developer's checkout path makes the identity — and therefore every
// baseline — checkout-bound, so a macOS checkout and Ubuntu CI could never
// share one image set.
//
// Instead, receipt runs stage the neutral-demo bundles at this fixed,
// documented root before booting the dev instance
// (scripts/stage-receipt-fixtures.sh), and `make dev` / `make dev-api`
// honor the staged paths when the fixture env vars are pre-set. The
// server's production repository-identity rule is unchanged
// (internal/sync RepoName: `local/` + the absolute bundle path without its
// leading slash); only the receipt environment's input to that rule is
// fixed. The string is identical on macOS and Linux, so one baseline set
// serves every supported renderer — baselines are produced in the Ubuntu CI
// rendering environment (see ui/receipts/README.md).
//
// This constant is the single source of truth: the staging script extracts
// it from this file, and routes.ts derives every neutral-demo repository
// identity from it. Keep it an absolute POSIX path with no trailing slash.
export const RECEIPT_FIXTURE_ROOT = '/tmp/phebs-receipts-fixtures'

// Neutral-demo bundle file names, staged at RECEIPT_FIXTURE_ROOT.
export const T307_BUNDLE = 't307-neutral-service.bundle'
export const T323_BUNDLE = 't323-neutral-corpus.bundle'

// Mirror of the server's local-path RepoName rule for an absolute, clean
// POSIX bundle path: `local/` + the path without its leading slash.
// safeName applies no transformation (internal/sync/sync.go), so this is
// exact for the staged fixture paths.
export function receiptFixtureRepoName(bundleFileName: string): string {
  return `local/${RECEIPT_FIXTURE_ROOT.replace(/^\/+/, '')}/${bundleFileName}`
}
