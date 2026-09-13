package t421

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/bmeddeb/phebs/internal/custodybytes"
	"github.com/bmeddeb/phebs/internal/dispatchadmission"
	"github.com/bmeddeb/phebs/internal/readaccounting"
	"github.com/bmeddeb/phebs/internal/storeaccounting"
)

func receiptMetricTestEvidence(t *testing.T) (Plan, executionJoinedWork, dispatchadmission.Snapshot, storeaccounting.WireSnapshot, []ExecutionPhaseInspection, []ExecutionServerProcessObservation) {
	t.Helper()
	plan := logicalStoreWorkTestPlan(t)
	work := joinedMetricsTestWork(plan)
	for recordIndex := range work.Records {
		record := &work.Records[recordIndex]
		record.Attempts.WorkspaceBytes = ExecutionWorkspaceByteObservation{Bound: true, Complete: true}
		for _, phase := range executionProducerPhases(record.Producer) {
			record.Attempts.WorkspaceBytes.Phases[phase-1] = ExecutionWorkspaceBytePhase{
				Attempts: 1, Completed: 1,
				Maximum: custodybytes.Sample{LogicalBytes: uint64(100 + phase + record.Producer), AllocatedBytes: uint64(200 + phase + record.Producer)},
			}
		}
	}
	dispatch := dispatchadmission.Snapshot{Complete: true}
	for index := 0; index < executionProducerCount; index++ {
		dispatch.Producers = append(dispatch.Producers, dispatchadmission.ProducerCount{Producer: uint32(index + 1), Attached: true, Closed: true})
	}
	for index := range plan.PhaseOrder {
		row := dispatchadmission.PhaseCount{Phase: uint32(index + 1)}
		for _, name := range plan.WorkEnvelope.ControlledDispatchRoles {
			if role := executionDispatchRole(name); role != 0 {
				row.Roles = append(row.Roles, dispatchadmission.RoleCount{Role: role})
			}
		}
		slices.SortFunc(row.Roles, func(a, b dispatchadmission.RoleCount) int { return int(a.Role) - int(b.Role) })
		dispatch.Phases = append(dispatch.Phases, row)
	}
	store := storeaccounting.WireSnapshot{Opened: 7, TerminalEOF: 7, PrefixesClosed: true,
		Store: storeaccounting.Snapshot{Phase: 15, PrefixesClosed: true}}
	for _, producer := range []uint32{2, 3, 4, 5, 6, 10, 11} {
		row := storeaccounting.ProducerCount{Producer: producer, Attached: true, Closed: true}
		if producer == 4 {
			row.Closed, row.TerminalFencedEOF, row.TerminalPhase = false, true, 8
		}
		store.Store.Producers = append(store.Store.Producers, row)
	}
	for index := range plan.PhaseOrder {
		row := storeaccounting.PhaseCount{Phase: uint32(index + 1)}
		if index == 1 {
			row.Transactions, row.Rows, row.MaximumRows = 2, 3, 2
			store.Store.Transactions, store.Store.Rows, store.Store.MaximumRows = 2, 3, 2
		}
		store.Store.Phases = append(store.Store.Phases, row)
	}
	wantPhases := [...]uint32{2, 3, 4, 5, 6, 7, 8, 8, 9, 10, 11, 12, 13, 14}
	wantEpoch := [...]uint64{1, 1, 1, 2, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5}
	inspection := make([]ExecutionPhaseInspection, len(wantEpoch))
	var epoch, next uint64
	for offset, serverEpoch := range wantEpoch {
		if serverEpoch != epoch {
			epoch, next = serverEpoch, 1
		}
		phase := plan.PhaseOrder[wantPhases[offset]-1]
		inspection[offset] = ExecutionPhaseInspection{ServerEpoch: serverEpoch, Phase: phase, FirstOrdinal: next,
			NextOrdinal: next + 1, AcceptedReports: 1}
		if offset != 6 {
			inspection[offset].SelectorAccepted = true
			inspection[offset].Final = &ExecutionInspectionFinal{Ordinal: next, Projection: PhaseStateProjection{Phase: phase}}
		}
		if logicalIndex := slices.Index([]string{"physical_delta_b", "logical_delta_b", "return_a"}, phase); logicalIndex >= 0 {
			priorIndex := 0
			if logicalIndex == 2 {
				priorIndex = 1
			}
			inspection[offset].LogicalChanges = ExecutionLogicalChangeObservation{Complete: true,
				Prior: plan.Revisions.Logical[priorIndex].CatalogSource, Current: plan.Revisions.Logical[logicalIndex].CatalogSource,
				ChangedAcceptedServices: plan.Revisions.Logical[logicalIndex].ChangedLogicalServices}
		}
		next++
	}
	inspection[0].Reads = readaccounting.Counts{ControlFileReads: 1, StoreReadAttempts: 2, MemberVisits: 3}
	inspection[6].Reads = readaccounting.Counts{ControlFileReads: 1, StoreReadAttempts: 2, MemberVisits: 3}
	inspection[7].Reads = readaccounting.Counts{ControlFileReads: 4, StoreReadAttempts: 5, MemberVisits: 6}
	process := func(phase uint32, censuses, rss uint64) ExecutionServerProcessPhase {
		return ExecutionServerProcessPhase{Phase: phase, Observation: ProcessObservation{
			MeasurementKind: "sampled_observation", NativeHistory: "not_established", SimultaneousBounds: "not_established",
			Available: true, CompletedCensuses: censuses, ObservedRSSBytes: rss, ObservedRSSHighWaterBytes: rss,
			Classes: []ProcessObservationClass{{Class: "phebs"}},
		}}
	}
	processes := make([]ExecutionServerProcessObservation, 5)
	for index, phases := range [][]uint32{{2, 3, 4}, {5}, {6, 7, 8}, {8, 9, 10, 11}, {12, 13, 14}} {
		processes[index].Joined = true
		for _, phase := range phases {
			censuses, rss := uint64(1), uint64(100+phase)
			if index == 3 && phase == 8 {
				censuses, rss = 2, 208
			}
			processes[index].Phases = append(processes[index].Phases, process(phase, censuses, rss))
		}
	}
	return plan, work, dispatch, store, inspection, processes
}

func TestComposeExecutionReceiptMetricsUsesOnlyJoinedEvidence(t *testing.T) {
	plan, work, dispatch, store, inspection, processes := receiptMetricTestEvidence(t)
	got, err := composeExecutionReceiptMetrics(plan, work, dispatch, store, inspection, processes)
	if err != nil {
		t.Fatal(err)
	}
	if got.JoinedFamilies[0] || got.JoinedFamilies[14] {
		t.Fatal("unowned boundary phases became fully measured")
	}
	for index := 1; index < 14; index++ {
		if !got.JoinedFamilies[index] {
			t.Fatalf("phase %d lost joined coverage: %+v", index+1, got.Coverage[index])
		}
	}
	phaseTwo := got.Metrics[1]
	if phaseTwo.ControlReads != 3 || phaseTwo.MemberReads != 3 || phaseTwo.StoreTransactions != 2 ||
		phaseTwo.StoreRows != 3 || phaseTwo.MaxRowsTransaction != 2 || !phaseTwo.AllocationMeasurementAvailable ||
		phaseTwo.DataLogicalBytes != 104 || phaseTwo.DataAllocatedBytes != 204 || !phaseTwo.DispatchMeasurementAvailable ||
		!phaseTwo.NativeMeasurementAvailable || phaseTwo.ObservedRSSHighWaterBytes != 102 {
		t.Fatalf("phase-two evidence was not projected exactly: %+v", phaseTwo)
	}
	if got.Metrics[7].DataLogicalBytes != 113 || got.Metrics[7].DataAllocatedBytes != 213 ||
		got.Metrics[7].ObservedRSSHighWaterBytes != 208 || got.Native[7].CompletedCensuses != 2 ||
		got.Metrics[7].ControlReads != 12 || got.Metrics[7].MemberReads != 9 {
		t.Fatalf("phase-eight successor/max composition differs: %+v %+v", got.Metrics[7], got.Native[7])
	}
	if got.Metrics[3].ChangedLogicalServices != 0 ||
		got.Metrics[4].ChangedLogicalServices != CountMetric(plan.Revisions.Logical[1].ChangedLogicalServices) ||
		got.Metrics[5].ChangedLogicalServices != CountMetric(plan.Revisions.Logical[2].ChangedLogicalServices) {
		t.Fatal("accepted logical-change evidence was not projected exactly")
	}
	if len(got.Dispatch[1].Roles) != 8 || got.Dispatch[1].Roles[4].Name == "" {
		t.Fatal("closed dispatch role inventory was not retained", got.Dispatch[1].Roles)
	}
	processes[3].Phases[0].Observation.Classes[0].Class = "git"
	if got.Native[7].Classes[0].Class != "phebs" {
		t.Fatal("returned native evidence aliases caller storage")
	}
}

func TestComposeExecutionReceiptMetricsRefusesIncompleteOrIncoherentFamilies(t *testing.T) {
	basePlan, baseWork, baseDispatch, baseStore, baseInspection, baseProcesses := receiptMetricTestEvidence(t)
	for _, test := range []struct {
		name string
		edit func(*Plan, *executionJoinedWork, *dispatchadmission.Snapshot, *storeaccounting.WireSnapshot, *[]ExecutionPhaseInspection, *[]ExecutionServerProcessObservation)
	}{
		{"dispatch prefix", func(_ *Plan, _ *executionJoinedWork, value *dispatchadmission.Snapshot, _ *storeaccounting.WireSnapshot, _ *[]ExecutionPhaseInspection, _ *[]ExecutionServerProcessObservation) {
			value.Complete = false
		}},
		{"plan mutation", func(value *Plan, _ *executionJoinedWork, _ *dispatchadmission.Snapshot, _ *storeaccounting.WireSnapshot, _ *[]ExecutionPhaseInspection, _ *[]ExecutionServerProcessObservation) {
			value.WorkEnvelope.MaximumRetriesPerUnit++
		}},
		{"dispatch sum", func(_ *Plan, _ *executionJoinedWork, value *dispatchadmission.Snapshot, _ *storeaccounting.WireSnapshot, _ *[]ExecutionPhaseInspection, _ *[]ExecutionServerProcessObservation) {
			value.Phases[0].Attempts = 1
		}},
		{"store sum", func(_ *Plan, _ *executionJoinedWork, _ *dispatchadmission.Snapshot, value *storeaccounting.WireSnapshot, _ *[]ExecutionPhaseInspection, _ *[]ExecutionServerProcessObservation) {
			value.Store.Rows++
		}},
		{"inspection overflow", func(_ *Plan, _ *executionJoinedWork, _ *dispatchadmission.Snapshot, _ *storeaccounting.WireSnapshot, value *[]ExecutionPhaseInspection, _ *[]ExecutionServerProcessObservation) {
			(*value)[0].Reads.ControlFileReads, (*value)[0].Reads.StoreReadAttempts = math.MaxUint64, 1
		}},
		{"inspection write", func(_ *Plan, _ *executionJoinedWork, _ *dispatchadmission.Snapshot, _ *storeaccounting.WireSnapshot, value *[]ExecutionPhaseInspection, _ *[]ExecutionServerProcessObservation) {
			(*value)[0].Reads.StoreWriteAttempts = 1
		}},
		{"inspection ordinal hole", func(_ *Plan, _ *executionJoinedWork, _ *dispatchadmission.Snapshot, _ *storeaccounting.WireSnapshot, value *[]ExecutionPhaseInspection, _ *[]ExecutionServerProcessObservation) {
			(*value)[len(*value)-1].NextOrdinal++
		}},
		{"logical change", func(_ *Plan, _ *executionJoinedWork, _ *dispatchadmission.Snapshot, _ *storeaccounting.WireSnapshot, value *[]ExecutionPhaseInspection, _ *[]ExecutionServerProcessObservation) {
			(*value)[3].LogicalChanges.ChangedAcceptedServices++
		}},
		{"workspace gap", func(_ *Plan, value *executionJoinedWork, _ *dispatchadmission.Snapshot, _ *storeaccounting.WireSnapshot, _ *[]ExecutionPhaseInspection, _ *[]ExecutionServerProcessObservation) {
			value.Records[0].Attempts.WorkspaceBytes.Phases[1].Completed = 0
		}},
		{"process replacement", func(_ *Plan, _ *executionJoinedWork, _ *dispatchadmission.Snapshot, _ *storeaccounting.WireSnapshot, _ *[]ExecutionPhaseInspection, value *[]ExecutionServerProcessObservation) {
			(*value)[3].Phases[0].Observation.CompletedCensuses = 1
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan, work, dispatch, store := basePlan, baseWork, baseDispatch, baseStore
			dispatch.Phases, dispatch.Producers = slices.Clone(baseDispatch.Phases), slices.Clone(baseDispatch.Producers)
			for index := range dispatch.Phases {
				dispatch.Phases[index].Roles = slices.Clone(dispatch.Phases[index].Roles)
			}
			store.Store.Phases, store.Store.Producers = slices.Clone(baseStore.Store.Phases), slices.Clone(baseStore.Store.Producers)
			inspection := cloneInspectionEvidence(baseInspection)
			processes := make([]ExecutionServerProcessObservation, len(baseProcesses))
			for index, value := range baseProcesses {
				processes[index] = cloneServerProcessObservation(value)
			}
			test.edit(&plan, &work, &dispatch, &store, &inspection, &processes)
			if _, err := composeExecutionReceiptMetrics(plan, work, dispatch, store, inspection, processes); !errors.Is(err, errExecutionReceiptMetrics) {
				t.Fatal("invalid joined evidence was accepted", err)
			}
		})
	}
}
