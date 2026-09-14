//go:build darwin

package t421

import "slices"

func composeExecutionSequenceArchiveEvidence(plan Plan, sequence *executionEpochSequenceResult, measurements []PhaseMeasurement,
	authorities []AuthorityPhaseResult, events map[string]uint64,
) (*ArchiveTransition, error) {
	if sequence == nil {
		return nil, errExecutionReceiptTransition
	}
	authority := make(map[string]AuthorityPhaseResult, len(authorities))
	for _, row := range authorities {
		authority[row.Phase] = row
	}
	if authority["archive_restore"].Outcome != "passed" {
		return nil, nil
	}
	if sequence.current == nil || sequence.current.flow == nil {
		return nil, errExecutionReceiptTransition
	}
	inventories, err := executionArchiveStateInventories(sequence.current.flow.joinedWorkSnapshot())
	if err != nil {
		return nil, err
	}
	manifest := sequence.final.transitionObservations.archiveManifest
	if manifest == nil {
		return nil, errExecutionReceiptTransition
	}
	before, err := executionSequenceArchiveSemantic(sequence.restore, "pressure_75")
	if err != nil {
		return nil, err
	}
	after, err := executionSequenceArchiveSemantic(sequence.final, "archive_restore")
	if err != nil {
		return nil, err
	}
	meter := slices.IndexFunc(measurements, func(row PhaseMeasurement) bool { return row.Phase == "archive_restore" })
	if meter < 0 {
		return nil, errExecutionReceiptTransition
	}
	value, err := composeExecutionArchiveTransition(plan, measurements[meter], authority, executionArchiveReceiptObservation{
		Manifest: *manifest, StateInventories: inventories, Before: before, After: after,
		InstallationDestroyed: sequence.restore.ArchiveInstallationDestroyed, RestoreTargetEmpty: sequence.restore.ArchiveRestoreTargetEmpty,
		ScratchSourceAbsent: sequence.restore.ArchiveScratchSourceAbsent,
	}, events)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func executionSequenceArchiveSemantic(server ExecutionEpochOneResult, phase string) (executionArchiveSemanticObservation, error) {
	index := slices.IndexFunc(server.Inspection, func(row ExecutionPhaseInspection) bool { return row.Phase == phase })
	if index < 0 {
		return executionArchiveSemanticObservation{}, errExecutionReceiptTransition
	}
	row := server.Inspection[index]
	if !row.SelectorAccepted || row.Final == nil || row.Final.Projection.Phase != phase || row.Final.CallerPublication == nil {
		return executionArchiveSemanticObservation{}, errExecutionReceiptTransition
	}
	native := row.Final.CallerPublication
	if !native.valid() || native.GenerationSHA256 != row.Final.Authority.CallerGenerationSHA256 || native.ManifestSHA256 != row.Final.Authority.CallerRootSHA256 {
		return executionArchiveSemanticObservation{}, errExecutionReceiptTransition
	}
	observed := executionArchiveSemanticObservation{Projection: cloneInspectionFinal(*row.Final).Projection, CallerProjection: native.RPCProjection}
	for _, leaf := range native.Leaves {
		if leaf.Domain != "grpc-caller" {
			continue
		}
		observed.CallerLeaves = append(observed.CallerLeaves, CallerPublicationLeafResult{Prefix: leaf.Prefix, CandidateRecords: leaf.CandidateRecords, Outcome: "success",
			ResolvedPostings: leaf.Results, Abstentions: leaf.Abstentions, Records: leaf.Records, Unresolved: leaf.Unresolved,
			CanonicalBytes: leaf.ContentBytes, EncodedBytes: leaf.ContentBytes, ResultSHA256: leaf.ContentSHA256})
	}
	return observed, nil
}
