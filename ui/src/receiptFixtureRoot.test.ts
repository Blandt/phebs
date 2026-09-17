import { describe, expect, it } from 'vitest'
import {
  RECEIPT_FIXTURE_ROOT,
  T307_BUNDLE,
  T323_BUNDLE,
  receiptFixtureRepoName,
} from '../receipts/fixtureRoot'
import { ROUTES } from '../receipts/routes'

// Neutral-demo repository identities are receipt-environment-bound, never
// checkout-bound: the staged bundles live at RECEIPT_FIXTURE_ROOT on every
// machine, and the server derives the same `local/...` names there. These
// assertions pin the exact deterministic strings so any reintroduction of
// checkout-derived names fails loudly instead of silently invalidating
// every committed baseline.
describe('deterministic receipt fixture identities', () => {
  it('fixture root is the fixed absolute POSIX path', () => {
    expect(RECEIPT_FIXTURE_ROOT).toBe('/tmp/phebs-receipts-fixtures')
  })

  it('derives the exact server RepoName for the staged bundles', () => {
    expect(receiptFixtureRepoName(T307_BUNDLE)).toBe(
      'local/tmp/phebs-receipts-fixtures/t307-neutral-service.bundle',
    )
    expect(receiptFixtureRepoName(T323_BUNDLE)).toBe(
      'local/tmp/phebs-receipts-fixtures/t323-neutral-corpus.bundle',
    )
  })

  it('every routed repository identity is fixture-root-bound', () => {
    const identities = ROUTES.flatMap((route) => {
      const query = route.path.split('?')[1] ?? ''
      return [...new URLSearchParams(query).entries()]
        .filter(([key]) => key === 'repo' || key === 'repository')
        .map(([, value]) => value)
    })
    expect(identities.length).toBeGreaterThan(0)
    for (const identity of identities) {
      // The markdown-preview route is served by a page-scoped synthetic
      // fixture that never enters instance repository state.
      if (identity === 'receipt-fixture/markdown-preview') continue
      expect(identity).toMatch(/^local\/tmp\/phebs-receipts-fixtures\//)
    }
  })

  it('no route leaks a checkout-derived identity', () => {
    for (const route of ROUTES) {
      expect(route.path).not.toMatch(/local\/(Users|home|root|private)\//)
    }
  })
})
