package t421

import (
	"github.com/bmeddeb/phebs/internal/callerleaf"
	"github.com/bmeddeb/phebs/internal/callerpublication"
)

// Detached source-free quantities from an actual leased caller publication.
// Its per-leaf unresolved counts were observed during native cold validation.
type ExecutionCallerPublicationObservation struct {
	RelationshipRootReads       uint64                              `json:"relationship_root_reads"`
	RelationshipGenerationReads uint64                              `json:"relationship_generation_reads"`
	GenerationSHA256            string                              `json:"generation_sha256"`
	ManifestSHA256              string                              `json:"manifest_sha256"`
	Leaves                      []callerpublication.LeafObservation `json:"leaves"`
	RPCProjection               SetIdentity                         `json:"rpc_projection"`
}

func (value ExecutionCallerPublicationObservation) valid() bool {
	if !validDigest(value.GenerationSHA256) || !validDigest(value.ManifestSHA256) || value.Leaves == nil || len(value.Leaves) > callerleaf.MaxExpectedPairs || !validSetIdentity(value.RPCProjection) {
		return false
	}
	for _, leaf := range value.Leaves {
		if leaf.Domain == "" || leaf.Prefix == "" || !validDigest(leaf.ContentSHA256) || leaf.Unresolved > leaf.Abstentions ||
			leaf.Results > callerleaf.MaxResultRecordsPerPair || leaf.Abstentions > callerleaf.MaxAbstentionRecordsPerPair ||
			leaf.Records > callerleaf.MaxResultRecordsPerPair+callerleaf.MaxAbstentionRecordsPerPair+1 ||
			leaf.ContentBytes > uint64(callerleaf.MaxCanonicalBytesPerPair) {
			return false
		}
	}
	return true
}
