package t421

import (
	"fmt"
	"strings"
	"testing"
)

func TestExecutionReuseObservationDistinguishesIncompleteZeroAndDecision(t *testing.T) {
	plan := accountingTestPlan(t)
	bindings := attemptTestBindings()
	for _, phase := range executionProducerPhases(2) {
		bindings = strings.Replace(bindings, fmt.Sprintf("RU1:2:%X:00000\n", phase), "", 1)
	}
	raw := bindings + "RU1:2:2:ccccc\nRU1:2:3:00000\nRU1:2:4:00000\n"
	got, err := observeExecutionAttempts([]byte(raw), plan, 2, [32]byte{1}, true)
	if err != nil || !got.Reuse.Bound || !got.Reuse.Complete {
		t.Fatal(got.Reuse, err)
	}
	current := ExecutionReusePhase{
		Source: ExecutionReuseCurrent, Search: ExecutionReuseCurrent,
		Observation: ExecutionReuseCurrent, Catalog: ExecutionReuseCurrent,
		Relationship: ExecutionReuseCurrent, Complete: true,
	}
	if got.Reuse.Phases[1] != current || got.Reuse.Phases[2] != (ExecutionReusePhase{Complete: true}) ||
		got.Reuse.Phases[3] != (ExecutionReusePhase{Complete: true}) {
		t.Fatal("reuse incomplete/zero/decision distinction changed", got.Reuse)
	}
}

func TestExecutionReuseObservationRejectsIncoherentNativeRecords(t *testing.T) {
	plan := accountingTestPlan(t)
	bindings := attemptTestBindings()
	for _, phase := range executionProducerPhases(2) {
		bindings = strings.Replace(bindings, fmt.Sprintf("RU1:2:%X:00000\n", phase), "", 1)
	}
	for _, test := range []struct {
		name, raw string
	}{
		{"duplicate terminal", "RU1:2:2:00000\nRU1:2:2:00000\n"},
		{"split index decision", "RU1:2:2:c0000\n"},
		{"reactivation outside index", "RU1:2:2:ccp00\n"},
		{"unknown decision", "RU1:2:2:ccx00\n"},
		{"wrong producer", "RU1:3:2:00000\n"},
		{"wrong phase", "RU1:2:8:00000\n"},
		{"partial", "RU1:2:2:00000"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := observeExecutionAttempts([]byte(bindings+test.raw), plan, 2, [32]byte{1}, true)
			if err == nil || got.Reuse.Complete {
				t.Fatal(got.Reuse, err)
			}
		})
	}
	withoutBinding := strings.Replace(bindings, "RUB1:2:sha256:01"+strings.Repeat("00", 31)+"\n", "", 1)
	if got, err := observeExecutionAttempts([]byte(withoutBinding+"RU1:2:2:pp000\nRU1:2:3:00000\nRU1:2:4:00000\n"), plan, 2, [32]byte{1}, true); err == nil || got.Reuse.Complete {
		t.Fatal(got.Reuse, err)
	}
	if got, err := observeExecutionAttempts([]byte(bindings+"RU1:2:2:pp000\nRU1:2:3:00000\n"), plan, 2, [32]byte{1}, true); err == nil || got.Reuse.Complete || !got.Reuse.Phases[1].Complete {
		t.Fatal("missing terminal did not retain an incomplete prefix", got.Reuse, err)
	}
}
