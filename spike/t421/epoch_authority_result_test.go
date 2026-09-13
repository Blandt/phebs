package t421

import (
	"reflect"
	"testing"
)

func TestExecutionAcceptedAuthorityProjection(t *testing.T) {
	plan := accountingTestPlan(t)
	phases := plan.PhaseOrder[1 : len(plan.PhaseOrder)-1]
	roots := make([]ExtractionRootResult, len(plan.ReceiptContract.ExtractionDomains))
	for index, domain := range plan.ReceiptContract.ExtractionDomains {
		roots[index] = ExtractionRootResult{Domain: domain}
	}
	roots[0].PartitionResults = []ExtractionPartitionResult{{Ordinal: 1, ResultDigestSHA256: testDigest("result")}}
	state := AuthorityState{Current: true, ExtractionRootsSHA256: testDigest("roots")}
	flow := &ExecutionEpochOne{plan: plan}
	run := &ExecutionEpochOneRun{flow: flow, control: modeledQueryReceiptControl(t, false)}
	first := AuthorityPhaseResult{Phase: phases[0], Outcome: "passed", AuthorityState: state,
		ExtractionRoots: cloneExecutionAuthorityResult(AuthorityPhaseResult{ExtractionRoots: roots}).ExtractionRoots}
	reader := &executionEpochInspection{run: run, plan: plan, projection: PhaseStateProjection{Phase: phases[0]}, finalAuthority: first,
		evidence: epochInspectionLedger{rows: []ExecutionPhaseInspection{{Phase: phases[0], Final: &ExecutionInspectionFinal{Authority: state}}}}}
	if err := reader.acceptInspectionPhase(t.Context()); err != nil || !reader.evidence.rows[0].SelectorAccepted {
		t.Fatal("actual accepted F did not enter the phase coordinator", err)
	}
	for _, phase := range phases[1:] {
		value := cloneExecutionAuthorityResult(first)
		value.Phase = phase
		if err := flow.retainAcceptedAuthority(run, value); err != nil {
			t.Fatal("ordered actual authority refused", phase, err)
		}
	}

	first.ExtractionRoots[0].PartitionResults[0].ResultDigestSHA256 = testDigest("caller mutation")
	got := flow.acceptedAuthorityPrefix()
	if len(got) != len(phases) {
		t.Fatal("operational authority inventory is incomplete", len(got))
	}
	for index, value := range got {
		if value.Phase != phases[index] || value.Outcome != "passed" || !value.Current {
			t.Fatal("authority order or outcome changed", index, value.Phase, value.Outcome)
		}
	}
	want := got[0].ExtractionRoots[0].PartitionResults[0].ResultDigestSHA256
	if want == first.ExtractionRoots[0].PartitionResults[0].ResultDigestSHA256 {
		t.Fatal("phase coordinator retained caller-owned root storage")
	}
	got[0].ExtractionRoots[0].PartitionResults[0].ResultDigestSHA256 = testDigest("returned mutation")
	if next := flow.acceptedAuthorityPrefix(); next[0].ExtractionRoots[0].PartitionResults[0].ResultDigestSHA256 != want {
		t.Fatal("authority prefix aliases returned storage")
	}
	if err := flow.retainAcceptedAuthority(run, cloneExecutionAuthorityResult(got[len(got)-1])); err == nil {
		t.Fatal("authority prefix accepted a fourteenth or duplicate phase")
	}

	done := make(chan struct{})
	close(done)
	waiter := &ExecutionEpochOneRun{flow: flow, done: done, result: ExecutionEpochOneResult{Authorities: flow.acceptedAuthorityPrefix()}}
	returned, err := waiter.Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	returned.Authorities[0].ExtractionRoots[0].PartitionResults[0].ResultDigestSHA256 = testDigest("wait mutation")
	again, err := waiter.Wait(t.Context())
	if err != nil || !reflect.DeepEqual(again.Authorities, flow.acceptedAuthorityPrefix()) {
		t.Fatal("Wait returned aliased authority storage", err)
	}
}
