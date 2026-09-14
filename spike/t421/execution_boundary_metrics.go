package t421

import (
	"reflect"

	"github.com/bmeddeb/phebs/internal/custodybytes"
)

// These are the already completed parent-owned workspace walks, including
// actual timed AuthorA boundaries and final teardown before custody removal.
// Server/offline samples remain independently joined by the primary composer.
func composeExecutionBoundaryWorkspaceMetrics(plan Plan, parent custodybytes.Snapshot, out *executionReceiptMetrics) bool {
	if out == nil || plan.Schema != PlanV3Schema || parent.Unavailable || !parent.Phases[0].Completed || !parent.Phases[14].Completed {
		return false
	}
	for index, row := range parent.Phases {
		if !row.Completed {
			continue
		}
		if row.Maximum.LogicalBytes > plan.WorkEnvelope.MaximumDataLogicalBytes || row.Maximum.AllocatedBytes > plan.SafetyEnvelope.MaximumDataAllocatedBytes {
			return false
		}
		out.Metrics[index].DataLogicalBytes = max(out.Metrics[index].DataLogicalBytes, Bytes(row.Maximum.LogicalBytes))
		out.Metrics[index].DataAllocatedBytes = max(out.Metrics[index].DataAllocatedBytes, Bytes(row.Maximum.AllocatedBytes))
		out.Metrics[index].AllocationMeasurementAvailable = true
		out.Coverage[index].Workspace = true
	}
	return true
}

// The preflight geometry fields are the admitted bounds named by the receipt
// contract. Its disk fields are the actual native freeze snapshot, not a later
// pressure target or whole-phase minimum. Authentication remains caller-owned.
func composeExecutionPreflightGeometry(plan Plan, freeze ExecutionFreeze, out *ReceiptMetrics) bool {
	if out == nil || plan.Schema != PlanV3Schema || freeze.Schema != plan.ToolPolicy.ExecutionFreezeSchema {
		return false
	}
	geometry, err := expectedExecutionPressureGeometry(plan, freeze.Host)
	if err != nil || !reflect.DeepEqual(geometry, freeze.Pressure) {
		return false
	}
	out.AvailableDiskBytes = Bytes(freeze.Host.PressureAvailableDiskBytes)
	out.TotalDiskBytes = Bytes(freeze.Host.PressureTotalDiskBytes)
	out.MinimumPrePressureUsedBytes = Bytes(geometry.MinimumPrePressureUsedBytes)
	out.MaximumPrePressureUsedBytes = Bytes(geometry.MaximumPrePressureUsedBytes)
	out.MinimumPrePressureAllocatedBytes = Bytes(geometry.MinimumPrePressureBytes)
	out.MaximumPrePressureAllocatedBytes = Bytes(geometry.MaximumPrePressureBytes)
	out.BallastCeilingBytes = Bytes(geometry.BallastCeilingBytes)
	out.PressureVolumeBytes = Bytes(geometry.PressureVolumeBytes)
	out.PressureCustodyMarginBytes = Bytes(geometry.CustodyMarginBytes)
	return true
}
