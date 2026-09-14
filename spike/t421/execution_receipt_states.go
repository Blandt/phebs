package t421

import (
	"errors"
)

var errExecutionReceiptStates = errors.New("execution receipt state observations are incomplete")

// composeExecutionReceiptStates projects actual accepted F responses and owned
// native server lifetimes. Expected digests are comparison targets only; the
// observed digest is always computed from the retained response projection.
func composeExecutionReceiptStates(plan Plan, freeze ExecutionFreeze, phases []PhaseResult,
	measurements []PhaseMeasurement, authorities []AuthorityPhaseResult, servers []ExecutionEpochOneResult,
) ([]ExactPhaseEvidence, error) {
	if plan.Schema != PlanV3Schema || validatePlan(plan, &plan.Revisions) != nil || len(servers) > 5 {
		return nil, errExecutionReceiptStates
	}
	outcomes, stopped, err := validateReceiptPhases(phases, plan)
	if err != nil {
		return nil, errExecutionReceiptStates
	}
	expected, err := expectedStateProjectionDigests(plan)
	if err != nil {
		return nil, errExecutionReceiptStates
	}
	operational := plan.PhaseOrder[1 : len(plan.PhaseOrder)-1]
	if len(authorities) != len(operational) {
		return nil, errExecutionReceiptStates
	}
	profileDigest, err := receiptSHA256(freeze.Profile)
	if err != nil {
		return nil, errExecutionReceiptStates
	}
	image := ""
	for _, tool := range freeze.Tools {
		if tool.Role == "phebs" {
			image = tool.SHA256
		}
	}
	if !validExecutionSHA256(image) {
		return nil, errExecutionReceiptStates
	}
	result := make([]ExactPhaseEvidence, len(operational))
	for index, phase := range operational {
		value := ExactPhaseEvidence{Phase: phase, Outcome: outcomes[phase], ExpectedSHA256: expected[phase]}
		epoch, launchPhase, ok := expectedPhaseRuntime(freeze.Profile.Epochs, phase)
		if !ok {
			return nil, errExecutionReceiptStates
		}
		var server *ExecutionEpochOneResult
		for slot := range servers {
			if servers[slot].ServerProcesses.ServerEpoch == epoch {
				if server != nil {
					return nil, errExecutionReceiptStates
				}
				server = &servers[slot]
			}
		}
		if value.Outcome == "not_run" {
			result[index] = value
			continue
		}
		if server == nil {
			if value.Outcome == "passed" {
				return nil, errExecutionReceiptStates
			}
			result[index] = value
			continue
		}
		// F and native start facts were accepted before cleanup. A failed
		// join is retained by teardown and does not erase those observations.
		if !server.RootStarted {
			return nil, errExecutionReceiptStates
		}
		if value.Outcome == "passed" || stopped != nil && stopped.Phase == phase && stopped.Code == "exact_oracle_mismatch" {
			var final *ExecutionInspectionFinal
			for _, row := range server.Inspection {
				if row.Phase != phase || row.ServerEpoch != epoch || row.Final == nil {
					continue
				}
				if final != nil || value.Outcome == "passed" && !row.SelectorAccepted ||
					row.Final.Ordinal < row.FirstOrdinal || row.Final.Ordinal >= row.NextOrdinal {
					return nil, errExecutionReceiptStates
				}
				final = row.Final
			}
			if final == nil || final.Projection.Phase != phase || authorities[index].Phase != phase ||
				final.Authority != authorities[index].AuthorityState {
				return nil, errExecutionReceiptStates
			}
			projectionDigest, err := receiptSHA256(final.Projection)
			if err != nil {
				return nil, errExecutionReceiptStates
			}
			authorityDigest, err := authoritySnapshotSHA256(final.Authority)
			if err != nil {
				return nil, errExecutionReceiptStates
			}
			observed := ObservedPhaseState{Schema: plan.ReceiptContract.StateObservationSchema,
				ProjectionSHA256: projectionDigest, AuthoritySnapshotSHA256: authorityDigest,
				SourceAuthorityRecipe: "source-generation-and-authored-tree-recipe-v1",
				AuthorityReader:       "current-root-then-generation-then-exact-member-inventory-v1",
				SemanticReader:        semanticReaderForObservationSchema(plan.ReceiptContract.StateObservationSchema)}
			if value.Outcome == "stopped" {
				projection := cloneInspectionFinal(*final).Projection
				observed.Projection = &projection
			}
			value.Observed = &observed
			value.ObservedProjectionSHA256 = projectionDigest
			value.ObservedSHA256, err = receiptSHA256(observed)
			if err != nil {
				return nil, errExecutionReceiptStates
			}
		}
		native := server.ServerProcesses
		nativeAvailable := validExecutionSHA256(native.NativeIdentitySHA256)
		if native.LaunchPhase != launchPhase || native.StartEventOrdinal <= 1 ||
			nativeAvailable && native.NativeIdentityEventOrdinal <= native.StartEventOrdinal ||
			max(native.HealthReadyEventOrdinal, native.HealthStoppedEventOrdinal) <= native.StartEventOrdinal ||
			native.HealthReadyEventOrdinal != 0 && native.HealthStoppedEventOrdinal != 0 || native.HealthElapsedMS == 0 ||
			value.Outcome == "passed" && (!nativeAvailable || native.HealthReadyEventOrdinal == 0) ||
			!nativeAvailable && (value.Outcome != "stopped" || value.Observed != nil) {
			return nil, errExecutionReceiptStates
		}
		runtime := PhaseRuntimeBinding{Schema: freeze.Profile.RuntimeBindingSchema, Phase: phase,
			ProfileSHA256: profileDigest, InvocationSHA256: freeze.Profile.InvocationSHA256,
			ProcessImageSHA256: image, ProcessIdentitySHA256: recipeDigest("t422-phebs-process-identity-v3", image, native.NativeIdentitySHA256),
			ServerEpoch: epoch, StartEventOrdinal: native.StartEventOrdinal}
		if !nativeAvailable {
			runtime.ProcessIdentitySHA256 = ""
		}
		if phase == launchPhase {
			elapsed := native.HealthElapsedMS
			outcome := "ready"
			if native.HealthStoppedEventOrdinal != 0 {
				outcome = "not_ready"
			}
			runtime.Startup = &ServerStartupEvidence{ServerEpoch: epoch, Outcome: outcome,
				FinishEventOrdinal: max(native.HealthReadyEventOrdinal, native.HealthStoppedEventOrdinal), ElapsedMS: &elapsed}
			runtime.OwnedStart = &OwnedServerStartEvidence{Schema: "t422-owned-server-start-v1", ServerEpoch: epoch,
				StartEventOrdinal: native.StartEventOrdinal, ProcessImageSHA256: image,
				NativeIdentitySHA256: native.NativeIdentitySHA256, NativeIdentityAvailable: nativeAvailable, OwnedStartSucceeded: server.RootStarted}
		}
		value.Runtime = &runtime
		value.RuntimeSHA256, err = receiptSHA256(runtime)
		if err != nil {
			return nil, errExecutionReceiptStates
		}
		result[index] = value
	}
	if err := validateExactPhaseEvidence(result, operational, outcomes, expected, authorities, measurements, freeze, stopped, plan); err != nil {
		return nil, errExecutionReceiptStates
	}
	return result, nil
}
