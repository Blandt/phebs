package t421

import "github.com/bmeddeb/phebs/internal/rpccallerposting"

// Present only when the exact native F walked all actual RPC postings.
// The accepted Final's authority identifies the measured immutable component.
type ExecutionRPCPostingObservation struct {
	Resolved   uint64 `json:"resolved"`
	NameMatch  uint64 `json:"name_match"`
	Unresolved uint64 `json:"unresolved"`
}

func (counts ExecutionRPCPostingObservation) valid() bool {
	const maximum = rpccallerposting.MaxPostings
	return counts.Resolved <= maximum && counts.NameMatch <= maximum-counts.Resolved &&
		counts.Unresolved <= maximum-counts.Resolved-counts.NameMatch
}
