package t421

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/bmeddeb/phebs/internal/storeaccounting"
)

func logicalStoreWorkTestPlan(t *testing.T) Plan {
	t.Helper()
	plan := accountingTestPlan(t)
	if err := applyLogicalStoreWorkCorrection(&plan); err != nil {
		t.Fatal(err)
	}
	if err := applySelectorHandoffCleanupCorrection(&plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestLogicalStoreWorkContractAndPriorBuilders(t *testing.T) {
	prior, plan := selectorCleanupTestPlan(t), logicalStoreWorkTestPlan(t)
	if err := ValidatePlan(plan); err != nil {
		t.Fatal(err)
	}
	want := prior.WorkEnvelope.Phases[4]
	want.StoreTransactions.Maximum, want.StoreRows.Maximum = 100_001, 51_200_002
	for index, row := range plan.WorkEnvelope.Phases {
		expected := prior.WorkEnvelope.Phases[index]
		if index == 4 {
			expected = want
		}
		if !reflect.DeepEqual(row, expected) {
			t.Fatalf("unexpected phase %s change", row.Phase)
		}
	}
	if plan.WorkEnvelope.MaximumStoreRowsPerTransaction != 512 ||
		plan.LogicalStoreWork.ClaimAttemptsMaximum != 64 ||
		!reflect.DeepEqual(plan.SelectorHandoffCleanup, prior.SelectorHandoffCleanup) {
		t.Fatal("native bounds or cleanup changed")
	}
	for _, build := range []func(string) (Plan, error){BuildPlanV3, BuildPlanV3WithSelectorCleanup, BuildPlanV3WithLogicalStoreWork} {
		built, err := build(plan.SourceCommit)
		if err != nil || ValidateFrozenPlan(built) != nil {
			t.Fatalf("canonical builder failed: %v", err)
		}
		raw, err := MarshalCanonical(built)
		if err != nil || bytes.Contains(raw, []byte(`"logical_store_work"`)) != (built.LogicalStoreWork != nil) {
			t.Fatal("optional policy encoding differs", err)
		}
	}
}

func TestLogicalStoreWorkRefusesMutations(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Plan)
	}{
		{"unknown", func(p *Plan) { p.LogicalStoreWork.Schema = "unknown" }},
		{"phase", func(p *Plan) { p.LogicalStoreWork.Phase = "cold" }},
		{"reference", func(p *Plan) { p.LogicalStoreWork.ReferencePhase = "warm_noop" }},
		{"claim", func(p *Plan) { p.LogicalStoreWork.ClaimAttemptsMaximum++ }},
		{"base", func(p *Plan) { p.LogicalStoreWork.BaseTransactions++ }},
		{"rows", func(p *Plan) { p.LogicalStoreWork.BaseRows = math.MaxUint64 }},
		{"description", func(p *Plan) { p.LogicalStoreWork.AccountingPolicy += "changed" }},
		{"omitted", func(p *Plan) { p.LogicalStoreWork = nil }},
		{"no_cleanup", func(p *Plan) { p.SelectorHandoffCleanup = nil }},
		{"wrong_schema", func(p *Plan) { p.Schema = PlanV2Schema }},
		{"lost_cleanup_transaction", func(p *Plan) { p.WorkEnvelope.Phases[4].StoreTransactions.Maximum-- }},
		{"lost_cleanup_rows", func(p *Plan) { p.WorkEnvelope.Phases[4].StoreRows.Maximum -= 2 }},
		{"overflow", func(p *Plan) { p.WorkEnvelope.Phases[4].StoreTransactions.Maximum = math.MaxUint64 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := logicalStoreWorkTestPlan(t)
			test.mutate(&plan)
			// Only contract fields mutate here. Reuse the retained revisions;
			// the separate builder test rederives the full source identities.
			if validatePlan(plan, &plan.Revisions) == nil {
				t.Fatal("changed policy admitted")
			}
		})
	}
	for _, plan := range []Plan{{}, selectorCleanupTestPlan(t), logicalStoreWorkTestPlan(t)} {
		if applyLogicalStoreWorkCorrection(&plan) == nil {
			t.Fatal("invalid correction order admitted")
		}
	}
}

func TestLogicalStoreWorkSAProjection(t *testing.T) {
	plan := logicalStoreWorkTestPlan(t)
	config, wire, err := executionStoreConfig(plan, testExecutionDispatchBindings())
	if err != nil {
		t.Fatal(err)
	}
	prior, oldWire, err := executionStoreConfig(selectorCleanupTestPlan(t), testExecutionDispatchBindings())
	if err != nil || !reflect.DeepEqual(wire, oldWire) || !reflect.DeepEqual(config.Producers, prior.Producers) {
		t.Fatal("producer topology or ACK changed", err)
	}
	var totalTransactions, totalRows uint64
	for index, row := range config.Phases {
		want := prior.Phases[index]
		if index == 4 {
			want.Transactions, want.Rows = 100_001, 51_200_002
		}
		if row != want {
			t.Fatalf("wrong SA phase projection: %+v", row)
		}
		totalTransactions += row.Transactions
		totalRows += row.Rows
	}
	controller, err := storeaccounting.New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := storeaccounting.NewTransport(t.Context(), controller, wire)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := transport.Close(); !errors.Is(err, storeaccounting.ErrIncomplete) {
			t.Errorf("unopened transport close: %v", err)
		}
	}()
	snapshot, err := transport.Snapshot()
	// Independent closed arithmetic: retained base, logical replacement,
	// four selector handoffs, then three selected-cleanup phase reserves.
	const wantTransactions = uint64(507170 + (100000 - 170) + 1254 + 3*4096*(2+65))
	const wantRows = uint64(259671040 + (100000-170)*512 + 20004 + 3*4096*(2+512))
	// SA01 Submit/Settle pairs, sixteen phase checkpoints and four controls
	// for each of seven lifetimes. This is allowance, never measured traffic.
	const wantWireBytes = (4*wantTransactions + 2*min(wantRows, 512*wantTransactions) + 16 + 4*7) * 2 * storeaccounting.FrameBytes
	if err != nil || totalTransactions != wantTransactions || totalRows != wantRows || snapshot.MaximumBytes != wantWireBytes {
		t.Fatalf("wrong closed store/wire allowance: transactions=%d rows=%d bytes=%d: %v",
			totalTransactions, totalRows, snapshot.MaximumBytes, err)
	}
	if snapshot.ReservedBytes != 0 || snapshot.Opened != 0 || snapshot.TerminalEOF != 0 || snapshot.Complete || snapshot.PrefixesClosed ||
		snapshot.Store.Transactions != 0 || snapshot.Store.Rows != 0 || snapshot.Store.MaximumRows != 0 || snapshot.Store.Complete || snapshot.Store.PrefixesClosed {
		t.Fatalf("allowance invented work, wire reservation or closure: %+v", snapshot)
	}
	t.Logf("prospective global SA wire maximum=%d; no bytes reserved", snapshot.MaximumBytes)
}

func TestLogicalStoreWorkEpochInput(t *testing.T) {
	epoch := ExecutionEpochConfig{Epoch: 2, ConfigSHA256: testDigest("config"), Repository: "example.com/mono", SelectorHandoffCleanup: SelectorHandoffCleanupSchema}
	old, err := epochSemanticInput(testDigest("plan"), epoch, nil)
	if err != nil || bytes.Contains(old, []byte("logical_store_work")) {
		t.Fatal("omitted epoch input differs", err)
	}
	epoch.LogicalStoreWork = LogicalStoreWorkSchema
	raw, err := epochSemanticInput(testDigest("plan"), epoch, nil)
	if err != nil || !bytes.Contains(raw, []byte(`"logical_store_work":"`+LogicalStoreWorkSchema+`"`)) || sha256.Sum256(raw) == sha256.Sum256(old) {
		t.Fatal("prospective input not bound", err)
	}
	for _, mode := range []string{"unknown", "nonlogical", "no_cleanup"} {
		value := epoch
		switch mode {
		case "unknown":
			value.LogicalStoreWork = "unknown"
		case "nonlogical":
			value.Epoch = 1
		case "no_cleanup":
			value.SelectorHandoffCleanup = ""
		}
		if _, err := epochSemanticInput(testDigest("plan"), value, nil); err == nil {
			t.Fatal("invalid epoch policy admitted", mode)
		}
	}
}
