package t421

import (
	"reflect"
	"testing"

	"github.com/bmeddeb/phebs/internal/dispatchadmission"
)

func TestExecutionRevisionResultsUseJoinedAuthorEvidence(t *testing.T) {
	plan := accountingTestPlan(t)
	outcomes := make(map[string]string, len(plan.PhaseOrder))
	for _, phase := range plan.PhaseOrder {
		outcomes[phase] = "passed"
	}
	want := completeTestRevisionResults(t, plan, outcomes)
	authored := make([]ExecutionAuthorResult, len(want))
	for index, value := range want {
		authored[index] = ExecutionAuthorResult{
			Revision: value.Name, ProducerID: uint32(7 + index), RootStarted: true,
			RootJoined: true, SessionEmpty: true, Completed: true,
			Response: &ExecutionCorpusAuthorResponse{Result: AuthoredExecutionRevision{
				Name: value.Name, Commit: value.PhysicalCommit, Tree: value.PhysicalTree,
				ParentCommit: value.PhysicalParentCommit, Manifest: value.AuthoredManifest,
			}, ConfigSHA256: SHA256([]byte(value.Name))},
			Accounting: dispatchadmission.Snapshot{Producers: []dispatchadmission.ProducerCount{{
				Producer: uint32(7 + index), Attached: true, Closed: true, Ordinal: authorCustodyAttempts(index),
			}}},
		}
	}
	got, err := composeExecutionRevisionResults(plan, outcomes, authored)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("revision evidence = %+v; %v", got, err)
	}

	for _, mutate := range []func([]ExecutionAuthorResult){
		func(values []ExecutionAuthorResult) { values[1].RootJoined = false },
		func(values []ExecutionAuthorResult) { values[1].ProducerID = 9 },
		func(values []ExecutionAuthorResult) { values[1].Response.ConfigSHA256 = "" },
		func(values []ExecutionAuthorResult) {
			values[1].Response.Result.Commit = values[0].Response.Result.Commit
		},
	} {
		changed := make([]ExecutionAuthorResult, len(authored))
		for index, value := range authored {
			changed[index] = cloneAuthorCustodyResult(value)
		}
		mutate(changed)
		if _, err := composeExecutionRevisionResults(plan, outcomes, changed); err == nil {
			t.Fatal("changed author evidence admitted")
		}
	}
}

func TestExecutionRevisionResultsStoppedPrefix(t *testing.T) {
	plan := accountingTestPlan(t)
	for _, test := range []struct {
		name string
		stop int
	}{
		{"preflight", 0}, {"cold", 1}, {"warm", 2}, {"physical", 3}, {"logical", 4}, {"late", 13},
	} {
		t.Run(test.name, func(t *testing.T) {
			outcomes := make(map[string]string, len(plan.PhaseOrder))
			for i, phase := range plan.PhaseOrder {
				outcomes[phase] = "passed"
				if i == test.stop {
					outcomes[phase] = "stopped"
				}
				if i > test.stop {
					outcomes[phase] = "not_run"
				}
			}
			want := completeTestRevisionResults(t, plan, outcomes)
			var authors []ExecutionAuthorResult
			for i, row := range want {
				if row.PhysicalOutcome != "passed" {
					break
				}
				authors = append(authors, ExecutionAuthorResult{Revision: row.Name, ProducerID: uint32(7 + i), RootStarted: true, RootJoined: true, SessionEmpty: true, Completed: true,
					Response:   &ExecutionCorpusAuthorResponse{Result: AuthoredExecutionRevision{Name: row.Name, Commit: row.PhysicalCommit, Tree: row.PhysicalTree, ParentCommit: row.PhysicalParentCommit, Manifest: row.AuthoredManifest}, ConfigSHA256: SHA256([]byte(row.Name))},
					Accounting: dispatchadmission.Snapshot{Producers: []dispatchadmission.ProducerCount{{Producer: uint32(7 + i), Attached: true, Closed: true, Ordinal: authorCustodyAttempts(i)}}}})
			}
			got, err := composeExecutionRevisionResults(plan, outcomes, authors)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("prefix differs: %v", err)
			}
			if len(authors) > 0 {
				if _, err := composeExecutionRevisionResults(plan, outcomes, authors[:len(authors)-1]); err == nil {
					t.Fatal("missing completed author accepted")
				}
			}
		})
	}
}
