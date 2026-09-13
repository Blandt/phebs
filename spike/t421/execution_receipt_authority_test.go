package t421

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

func TestComposeExecutionReceiptAuthorityInventory(t *testing.T) {
	plan := clonePlan(t, correctedTestPlan(t))
	if err := applyProcessAccountingCorrection(&plan); err != nil {
		t.Fatal(err)
	}
	operational := plan.PhaseOrder[1 : len(plan.PhaseOrder)-1]
	roots := []ExtractionRootResult{{
		Domain: "go",
		PartitionResults: []ExtractionPartitionResult{{
			Ordinal: 1, ResultDigestSHA256: testDigest("result"),
		}},
	}}
	rootDigest := mustReceiptSHA256(t, roots)
	state := AuthorityState{
		PhysicalRevision: "a", LogicalRevision: "a",
		SourceGenerationSHA256: testDigest("source"), ExtractionRootsSHA256: rootDigest,
	}
	authorities := make([]AuthorityPhaseResult, len(operational))
	for index, phase := range operational {
		authorities[index] = AuthorityPhaseResult{
			Phase: phase, Outcome: "passed", AuthorityState: state,
			ExtractionRoots: cloneExecutionAuthorityResult(AuthorityPhaseResult{ExtractionRoots: roots}).ExtractionRoots,
		}
	}
	pristine := make([]AuthorityPhaseResult, len(authorities))
	for index, value := range authorities {
		pristine[index] = cloneExecutionAuthorityResult(value)
	}

	inventory, err := composeExecutionReceiptAuthorityInventory(plan, authorities)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := make(map[string]string, len(operational))
	for _, phase := range operational {
		outcomes[phase] = "passed"
	}
	resolved, err := resolveAuthorityResults(
		inventory.ExtractionRoots,
		inventory.Snapshots,
		inventory.Results,
		operational,
		outcomes,
	)
	if err != nil || !reflect.DeepEqual(resolved, authorities) {
		t.Fatal("canonical authority inventory did not round trip", err)
	}
	if len(inventory.Snapshots) >= len(authorities) || len(inventory.ExtractionRoots) >= len(authorities) {
		t.Fatal("repeated authority was not deduplicated")
	}
	wantRoot := inventory.ExtractionRoots[0].Roots[0].PartitionResults[0].ResultDigestSHA256
	authorities[0].ExtractionRoots[0].PartitionResults[0].ResultDigestSHA256 = testDigest("caller mutation")
	if inventory.ExtractionRoots[0].Roots[0].PartitionResults[0].ResultDigestSHA256 != wantRoot {
		t.Fatal("authority inventory aliases caller storage")
	}

	for _, test := range []struct {
		name string
		edit func([]AuthorityPhaseResult) []AuthorityPhaseResult
	}{
		{"missing", func(values []AuthorityPhaseResult) []AuthorityPhaseResult { return values[:len(values)-1] }},
		{"phase", func(values []AuthorityPhaseResult) []AuthorityPhaseResult {
			values[0].Phase = "warm_noop"
			return values
		}},
		{"root digest", func(values []AuthorityPhaseResult) []AuthorityPhaseResult {
			values[0].ExtractionRootsSHA256 = testDigest("other roots")
			return values
		}},
		{"not run before stop", func(values []AuthorityPhaseResult) []AuthorityPhaseResult {
			values[0] = AuthorityPhaseResult{Phase: values[0].Phase, Outcome: "not_run"}
			return values
		}},
		{"passed after stop", func(values []AuthorityPhaseResult) []AuthorityPhaseResult {
			values[0].Outcome = "stopped"
			return values
		}},
		{"not run data", func(values []AuthorityPhaseResult) []AuthorityPhaseResult {
			values[0].Outcome = "stopped"
			values[1].Outcome = "not_run"
			return values
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := make([]AuthorityPhaseResult, len(pristine))
			for index, value := range pristine {
				values[index] = cloneExecutionAuthorityResult(value)
			}
			values = test.edit(values)
			if _, err := composeExecutionReceiptAuthorityInventory(plan, values); !errors.Is(err, errExecutionReceiptAuthority) {
				t.Fatal("invalid authority inventory was accepted", err)
			}
		})
	}

	stopped := make([]AuthorityPhaseResult, len(pristine))
	for index, value := range pristine {
		stopped[index] = cloneExecutionAuthorityResult(value)
	}
	stopped[1].Outcome = "stopped"
	for index := 2; index < len(stopped); index++ {
		stopped[index] = AuthorityPhaseResult{Phase: operational[index], Outcome: "not_run"}
	}
	stoppedInventory, err := composeExecutionReceiptAuthorityInventory(plan, stopped)
	if err != nil || stoppedInventory.Results[1].SnapshotSHA256 == "" ||
		!slices.EqualFunc(stoppedInventory.Results[2:], stopped[2:], func(reference AuthorityPhaseReference, value AuthorityPhaseResult) bool {
			return reference.Phase == value.Phase && reference.Outcome == "not_run" && reference.SnapshotSHA256 == ""
		}) {
		t.Fatal("stopped authority suffix did not retain the exact prefix", err)
	}
}
