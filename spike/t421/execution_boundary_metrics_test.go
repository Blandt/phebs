package t421

import (
	"testing"

	"github.com/bmeddeb/phebs/internal/custodybytes"
)

func TestComposeExecutionBoundaryWorkspacePreservesObservedMaxima(t *testing.T) {
	plan := logicalStoreWorkTestPlan(t)
	for _, tc := range []struct {
		name    string
		invalid bool
		mutate  func(*custodybytes.Phase, *custodybytes.Snapshot)
	}{
		{"complete", false, func(*custodybytes.Phase, *custodybytes.Snapshot) {}},
		{"missing prework does not erase real phase", false, func(p *custodybytes.Phase, _ *custodybytes.Snapshot) { p.Completed = false }},
		{"missing AuthorA", true, func(_ *custodybytes.Phase, p *custodybytes.Snapshot) { p.Phases[0].Completed = false }},
		{"missing teardown", true, func(_ *custodybytes.Phase, p *custodybytes.Snapshot) { p.Phases[14].Completed = false }},
		{"failed observation", true, func(_ *custodybytes.Phase, p *custodybytes.Snapshot) { p.Unavailable = true }},
		{"over bound", true, func(_ *custodybytes.Phase, p *custodybytes.Snapshot) {
			p.Phases[0].Maximum.LogicalBytes = plan.WorkEnvelope.MaximumDataLogicalBytes + 1
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preparation := custodybytes.Phase{Completed: true, Maximum: custodybytes.Sample{LogicalBytes: 120, AllocatedBytes: 160}}
			parent := custodybytes.Snapshot{}
			parent.Phases[0] = custodybytes.Phase{Completed: true, Maximum: custodybytes.Sample{LogicalBytes: 100, AllocatedBytes: 200}}
			parent.Phases[14] = custodybytes.Phase{Completed: true, Maximum: custodybytes.Sample{LogicalBytes: 90, AllocatedBytes: 110}}
			tc.mutate(&preparation, &parent)
			out := executionReceiptMetrics{}
			out.Metrics[14].DataLogicalBytes = 95
			if got := composeExecutionBoundaryWorkspaceMetrics(plan, parent, &out); got == tc.invalid {
				t.Fatalf("accepted=%v invalid=%v", got, tc.invalid)
			}
			if !tc.invalid && (out.Metrics[0].DataLogicalBytes != 100 || out.Metrics[0].DataAllocatedBytes != 200 || out.Metrics[14].DataLogicalBytes != 95 || !out.Coverage[14].Workspace || out.Coverage[1].Workspace) {
				t.Fatalf("wrong bounded join: %+v", out)
			}
		})
	}
}

func TestComposeExecutionPreflightGeometryUsesFreezeSample(t *testing.T) {
	plan := logicalStoreWorkTestPlan(t)
	freeze := ExecutionFreeze{Schema: plan.ToolPolicy.ExecutionFreezeSchema, Host: executionFreezeTestHost()}
	var err error
	freeze.Pressure, err = expectedExecutionPressureGeometry(plan, freeze.Host)
	if err != nil {
		t.Fatal(err)
	}
	metrics := ReceiptMetrics{}
	if !composeExecutionPreflightGeometry(plan, freeze, &metrics) || uint64(metrics.AvailableDiskBytes) != freeze.Host.PressureAvailableDiskBytes || uint64(metrics.MinimumPrePressureUsedBytes) != freeze.Pressure.MinimumPrePressureUsedBytes {
		t.Fatal("lost actual freeze geometry")
	}
	freeze.Pressure.MinimumPrePressureUsedBytes++
	if composeExecutionPreflightGeometry(plan, freeze, &metrics) {
		t.Fatal("accepted changed geometry")
	}
}
