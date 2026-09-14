//go:build darwin

package t421

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bmeddeb/phebs/internal/custodybytes"
	"github.com/bmeddeb/phebs/internal/dispatchadmission"
	"github.com/bmeddeb/phebs/internal/storeaccounting"
)

func TestExecutionObservedFailureKeepsActualPrimaryAndPrefix(t *testing.T) {
	plan := accountingTestPlan(t)
	for _, test := range []struct {
		name, code  string
		index       int
		unavailable []string
		change      func(*ReceiptMetrics)
	}{
		{name: "early missing disk", code: "measurement_unavailable", unavailable: []string{"available_disk_bytes"}},
		{name: "middle operation error", index: 6, code: "internal_error"},
		{name: "late native failure with observed RSS overshoot", index: 13, code: "observed_rss_ceiling", unavailable: []string{"observed_rss_high_water_bytes"}, change: func(m *ReceiptMetrics) {
			m.ObservedRSSHighWaterBytes = Bytes(plan.SafetyEnvelope.MaximumPeakRSSBytes + 17)
		}},
		{name: "work crossing survives failed reads", index: 5, code: "phase_work_limit", unavailable: slices.Clone(workUnavailableMetricGroups[0]), change: func(m *ReceiptMetrics) {
			m.JobAttempts = CountMetric(plan.WorkEnvelope.Phases[5].JobAttempts.Maximum + 9)
		}},
		{name: "multiple actual gauges", index: 12, code: "multiple_resource_ceilings", change: func(m *ReceiptMetrics) {
			m.ObservedRSSHighWaterBytes = Bytes(plan.SafetyEnvelope.MaximumPeakRSSBytes + 1)
			m.DataAllocatedBytes = Bytes(plan.SafetyEnvelope.MaximumDataAllocatedBytes + 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := make([]PhaseMeasurement, 15)
			for i, phase := range plan.PhaseOrder {
				rows[i] = PhaseMeasurement{Phase: phase, StartEventOrdinal: uint64(10 + i*10), FinishEventOrdinal: uint64(19 + i*10), Metrics: ReceiptMetrics{WallMS: 1}}
			}
			if test.change != nil {
				test.change(&rows[test.index].Metrics)
			}
			before := rows[test.index]
			failure, err := composeExecutionObservedFailure(plan, rows, test.index, uint64(15+test.index*10), test.unavailable)
			if err != nil || failure.Code != test.code {
				t.Fatalf("failure=%+v error=%v", failure, err)
			}
			if rows[test.index].Metrics != before.Metrics {
				t.Fatal("failure classification rewrote observed counters")
			}
			if _, _, err := expectedStoppedDecision(failure, rows, plan); err != nil {
				t.Fatal(err)
			}
			if err := validateStoppedFailureEvidence(Receipt{Measurements: rows}, &failure, nil, plan, ExecutionFreeze{}); err != nil {
				t.Fatal(err)
			}
			if _, err := composeExecutionObservedFailure(plan, rows, test.index, rows[test.index].FinishEventOrdinal, test.unavailable); err == nil {
				t.Fatal("accepted failure event outside observed phase")
			}
		})
	}
}

func TestExecutionSequenceArchiveNotRunNeedsNoServer(t *testing.T) {
	got, err := composeExecutionSequenceArchiveEvidence(Plan{}, &executionEpochSequenceResult{}, nil, []AuthorityPhaseResult{{Phase: "archive_restore", Outcome: "not_run"}}, nil)
	if err != nil || got != nil {
		t.Fatalf("not-run archive requires unstarted server: %v", err)
	}
}

// This closes the production wrapper over an early failed execution. The
// inputs model owner snapshots, not a completed receipt or corpus fixture.
func TestExecutionSequenceReceiptEarlyStoppedOwnerSnapshots(t *testing.T) {
	plan := accountingTestPlan(t)
	binding := frozenReceiptTestBinding(t, plan)
	flow := &ExecutionEpochOne{plan: plan, executionEvidenceEvents: map[string]uint64{"failure:preflight": 3}, executionPhaseEvents: &executionPhaseEventRecorder{phases: slices.Clone(plan.PhaseOrder), slots: make([]executionPhaseEventSlot, 15), active: -1, stopped: true}}
	flow.executionPhaseEvents.slots[0].value = PhaseMeasurement{Phase: "preflight", StartEventOrdinal: 2, FinishEventOrdinal: 4, Metrics: ReceiptMetrics{WallMS: 1}}
	flow.executionPhaseEvents.slots[14].value = PhaseMeasurement{Phase: "teardown", StartEventOrdinal: 5, FinishEventOrdinal: 20, Metrics: ReceiptMetrics{WallMS: 1}}
	sequence := &executionEpochSequenceResult{teardown: executionTeardownResult{Started: time.Now(), Joined: true, CleanupClosed: true, Store: storeaccounting.WireSnapshot{PrefixesClosed: true, Store: storeaccounting.Snapshot{Phase: 15, PrefixesClosed: true}}, Accounting: dispatchadmission.Snapshot{Complete: true}}}
	sequence.stopped.ImagePresent = true
	sequence.stopped.WorkspacePresent = true
	sequence.stopped.SourcePresent = true
	sequence.stopped.CustodyLockHeld = true
	for phase := uint32(1); phase <= 15; phase++ {
		row := dispatchadmission.PhaseCount{Phase: phase}
		for role := uint32(1); role <= 7; role++ {
			row.Roles = append(row.Roles, dispatchadmission.RoleCount{Role: role})
		}
		sequence.teardown.Accounting.Phases = append(sequence.teardown.Accounting.Phases, row)
		sequence.teardown.Store.Store.Phases = append(sequence.teardown.Store.Store.Phases, storeaccounting.PhaseCount{Phase: phase})
	}
	resources := executionWholeResourceEvidence{Joined: true}
	for _, index := range []int{0, 14} {
		resources.Native[index] = ProcessObservation{MeasurementKind: "sampled_observation", NativeHistory: "not_established", SimultaneousBounds: "not_established", Available: true, CompletedCensuses: 1, ObservedRSSBytes: 1024, ObservedRSSHighWaterBytes: 1024, Classes: []ProcessObservationClass{{Class: "t422-execute"}}}
	}
	resources.Disk[14] = executionDiskObservation{Samples: 2, Available: binding.freeze.Host.PressureAvailableDiskBytes, Total: binding.freeze.Host.PressureTotalDiskBytes}
	receipt, err := composeExecutionSequenceReceipt(plan, binding, sequence, flow, resources)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.PhaseResults[0].Outcome != "stopped" || receipt.PhaseResults[1].Outcome != "not_run" || receipt.Decision.Reason != "teardown_failed" || receipt.Teardown.PressureImagePaths != 1 {
		t.Fatalf("lost actual failed prefix: %+v", receipt.Decision)
	}
	if receipt.Measurements[0].Metrics.TotalDiskBytes != Bytes(binding.freeze.Host.PressureTotalDiskBytes) {
		t.Fatal("preflight lost authenticated native disk")
	}
	if receipt.Measurements[0].Metrics.ObservedRSSHighWaterBytes != 1024 {
		t.Fatal("positive native prefix lost")
	}
	if err := ValidateReceipt(receipt, plan, binding, ReturnedPackageBinding{}); err == nil {
		t.Fatal("unsigned wrapper receipt authenticated")
	}
	for _, test := range []struct {
		name   string
		change func()
	}{
		{"phase event", func() {
			flow.executionPhaseEvents.slots[1].value = PhaseMeasurement{Phase: "cold", StartEventOrdinal: 6, FinishEventOrdinal: 9}
		}},
		{"native census", func() { resources.Native[1] = resources.Native[0] }},
		{"accepted authority", func() { flow.authorities = []AuthorityPhaseResult{{Phase: "cold", Outcome: "passed"}} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.change()
			if _, err := composeExecutionSequenceReceipt(plan, binding, sequence, flow, resources); err == nil {
				t.Fatal("erased observed activity from not-run suffix")
			}
			flow.executionPhaseEvents.slots[1] = executionPhaseEventSlot{}
			resources.Native[1] = ProcessObservation{}
			flow.authorities = nil
		})
	}
}

func TestExecutionClosedPreflightUsesRealControllerAdvance(t *testing.T) {
	plan := accountingTestPlan(t)
	bindings := testExecutionDispatchBindings()
	config, err := executionDispatchConfig(plan, bindings)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := dispatchadmission.New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := controller.NewLocalProducer(t.Context(), executionRootProducer)
	if err != nil {
		t.Fatal(err)
	}
	sc, wc, err := executionStoreConfig(plan, bindings)
	if err != nil {
		t.Fatal(err)
	}
	store, err := storeaccounting.New(t.Context(), sc)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := storeaccounting.NewTransport(t.Context(), store, wc)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = wire.Close() }()
	if parent.Pause(t.Context()) != nil || controller.Fence() != nil || parent.Checkpoint(t.Context()) != nil || wire.Fence() != nil || wire.Advance() != nil || controller.Advance() != nil || parent.Resume(2) != nil {
		t.Fatal("real phase-one advance failed")
	}
	da, err := controller.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	sa, err := wire.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if da.Complete || sa.PrefixesClosed || sa.Store.Phase != 2 {
		t.Fatal("fixture did not retain live incomplete controllers")
	}
	prefix, err := composeExecutionStoppedMetricPrefix(plan, "cold", executionJoinedWork{}, da, sa, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(prefix.Unavailable[0], "controlled_dispatch_attempts") || !hasUnavailableStoreMetrics(prefix.Unavailable[0]) {
		t.Fatal("fixture lacks unclosed parent prefix")
	}
	composeExecutionClosedPreflightMetrics(da, sa, &prefix.Metrics, &prefix.Unavailable[0])
	if !prefix.Metrics.Dispatch[0].Complete || !prefix.Metrics.Coverage[0].Store || len(prefix.Unavailable[0]) != 0 || da.Complete || sa.PrefixesClosed {
		t.Fatal("actual preflight proof lost or global completion manufactured")
	}
	// Feed these genuinely incomplete snapshots through the whole production
	// wrapper after an actual bounded preflight workspace traversal.
	owner, ctx := custodyByteFixture(t)
	if err := os.WriteFile(filepath.Join(owner.path, "observed"), []byte("observed workspace bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	observer := custodybytes.NewBorrowed(owner.file, owner.path, owner.info, owner.volume)
	sample, err := observer.Sample(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	binding := frozenReceiptTestBinding(t, plan)
	flow := &ExecutionEpochOne{plan: plan, workspaceBytes: observer, executionEvidenceEvents: map[string]uint64{"failure:cold": 6}, executionPhaseEvents: &executionPhaseEventRecorder{phases: slices.Clone(plan.PhaseOrder), slots: make([]executionPhaseEventSlot, 15), active: -1, stopped: true}}
	for _, row := range []struct {
		index         int
		start, finish uint64
	}{{0, 2, 4}, {1, 5, 7}, {14, 8, 20}} {
		flow.executionPhaseEvents.slots[row.index].value = PhaseMeasurement{Phase: plan.PhaseOrder[row.index], StartEventOrdinal: row.start, FinishEventOrdinal: row.finish, Metrics: ReceiptMetrics{WallMS: 1}}
	}
	sequence := &executionEpochSequenceResult{teardown: executionTeardownResult{Started: time.Now(), Accounting: da, Store: sa}, stopped: executionStoppedTeardown{WorkspacePresent: true, ImagePresent: true, SourcePresent: true, CustodyLockHeld: true}}
	resources := executionWholeResourceEvidence{Joined: true}
	for _, index := range []int{0, 1, 14} {
		resources.Native[index] = ProcessObservation{MeasurementKind: "sampled_observation", NativeHistory: "not_established", SimultaneousBounds: "not_established", Available: true, CompletedCensuses: 1, ObservedRSSBytes: 1024, ObservedRSSHighWaterBytes: 1024, Classes: []ProcessObservationClass{{Class: "t422-execute"}}}
		resources.Disk[index] = executionDiskObservation{Samples: 1, Available: binding.freeze.Host.PressureAvailableDiskBytes, Total: binding.freeze.Host.PressureTotalDiskBytes}
	}
	receipt, err := composeExecutionSequenceReceipt(plan, binding, sequence, flow, resources)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.PhaseResults[0].Outcome != "passed" || receipt.PhaseResults[1].Outcome != "stopped" || receipt.Measurements[0].Metrics.DataLogicalBytes != Bytes(sample.LogicalBytes) || !receipt.Measurements[0].DispatchAccounting.Complete {
		t.Fatal("actual preflight was not preserved through cold stop")
	}
	for _, test := range []struct {
		name   string
		change func(*dispatchadmission.Snapshot, *storeaccounting.WireSnapshot)
		metric string
	}{
		{"missing root checkpoint", func(d *dispatchadmission.Snapshot, _ *storeaccounting.WireSnapshot) {
			for i := range d.Producers {
				if d.Producers[i].Producer == executionRootProducer {
					d.Producers[i].Checkpoint = 0
				}
			}
		}, "controlled_dispatch_attempts"},
		{"store not advanced", func(_ *dispatchadmission.Snapshot, s *storeaccounting.WireSnapshot) { s.Store.Phase = 1 }, "store_rows"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, s := da, sa
			d.Producers = slices.Clone(d.Producers)
			test.change(&d, &s)
			p, err := composeExecutionStoppedMetricPrefix(plan, "cold", executionJoinedWork{}, d, s, nil)
			if err != nil {
				t.Fatal(err)
			}
			composeExecutionClosedPreflightMetrics(d, s, &p.Metrics, &p.Unavailable[0])
			if !slices.Contains(p.Unavailable[0], test.metric) {
				t.Fatal("missing controller handoff repaired")
			}
		})
	}
}
