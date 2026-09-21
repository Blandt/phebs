package t421

import "testing"

func TestPressureTransitionSchemaIsVersioned(t *testing.T) {
	for _, test := range []struct {
		plan string
		want string
	}{
		{PlanSchema, "/pressure-v1"},
		{PlanV2Schema, "/pressure-v1"},
		{PlanV3Schema, "/pressure-v1"},
		{PlanV4Schema, "/pressure-v2"},
	} {
		t.Run(test.plan, func(t *testing.T) {
			plan := Plan{Schema: test.plan, ReceiptContract: ReceiptContract{TransitionSchema: "transition"}}
			if got := pressureTransitionSchema(plan); got != "transition"+test.want {
				t.Fatalf("schema = %q", got)
			}
		})
	}
}

func TestExecutionReceiptTransitionReadPrefixJoinsActualEpochs(t *testing.T) {
	subtotal := TransitionReadSubtotal{Schema: "t422-transition-read-accounting-v1", Class: "checkpoint-restart", ReportCalls: 1, ControlFileReads: 7, StoreReadAttempts: 2}
	servers := []ExecutionEpochOneResult{{Inspection: []ExecutionPhaseInspection{{Phase: "process_restart", TransitionReads: &subtotal}}}, {Inspection: []ExecutionPhaseInspection{{Phase: "process_restart", TransitionReads: &subtotal}}}}
	got, err := executionReceiptTransitionReadPrefix("process_restart", servers)
	if err != nil || got.ReportCalls != 2 || got.ControlFileReads != 14 || got.StoreReadAttempts != 4 {
		t.Fatalf("actual sum = %+v, %v", got, err)
	}
	for _, test := range []struct {
		name  string
		value TransitionReadSubtotal
	}{
		{"class", TransitionReadSubtotal{Schema: subtotal.Schema, Class: "other"}},
		{"overflow", TransitionReadSubtotal{Schema: subtotal.Schema, Class: subtotal.Class, ReportCalls: ^uint64(0)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := test.value
			servers[1].Inspection[0].TransitionReads = &changed
			if _, err := executionReceiptTransitionReadPrefix("process_restart", servers); err == nil {
				t.Fatal("invalid joined prefix accepted")
			}
		})
	}
	if _, err := executionReceiptTransitionReadPrefix("return_a", servers); err == nil {
		t.Fatal("missing actual reads accepted")
	}
}

func TestExecutionPressureTransitionNeedsActualMutations(t *testing.T) {
	plan := accountingTestPlan(t)
	for _, test := range []struct {
		name    string
		outcome string
		want    bool
	}{{"not run", "not_run", true}, {"stopped", "stopped", true}, {"passed", "passed", false}} {
		t.Run(test.name, func(t *testing.T) {
			got, err := composeExecutionPressureTransitions(plan, ExecutionFreeze{}, map[string]string{"pressure_80": test.outcome}, nil, epochPressureObservations{}, 0, nil)
			if (err == nil) != test.want || test.want && len(got) != 0 {
				t.Fatalf("pressure prefix = %v, %v", got, err)
			}
		})
	}
}
