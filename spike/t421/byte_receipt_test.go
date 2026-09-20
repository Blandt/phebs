package t421

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
)

// Bounded validator inputs only: no native constructor or claim that these
// values came from a filesystem traversal.
func TestByteReceiptFailedPrefix(t *testing.T) {
	for _, mode := range []string{"unavailable", "zero_prefix", "resource", "logical", "multiple", "topology", "work", "wrong_primary", "wrong_bound", "wrong_available", "missing_coverage", "unknown", "duplicate", "passed", "passed_zero"} {
		t.Run(mode, func(t *testing.T) {
			plan := accountingTestPlan(t)
			phase := "product_queries"
			index := slices.Index(plan.PhaseOrder, phase)
			values, outcomes, freeze := failureCompositionMeasurements(plan, phase)
			metrics := &values[index].Metrics
			metrics.DataAllocatedBytes, metrics.DataLogicalBytes = 23, 41
			metrics.AllocationMeasurementAvailable = false
			failure := ReceiptFailure{Phase: phase, Class: "internal", Code: "measurement_unavailable", Observation: FailureObservation{
				Schema: plan.ReceiptContract.FailureObservationSchema, Kind: "measurement_unavailable",
				UnavailableMetrics: []string{"data_allocated_bytes", "data_logical_bytes"},
			}}
			want, priority := true, uint64(4)
			switch mode {
			case "zero_prefix", "passed_zero":
				metrics.DataAllocatedBytes, metrics.DataLogicalBytes = 0, 0
				if mode == "passed_zero" {
					outcomes[phase], want = "passed", false
				}
			case "resource", "wrong_primary", "wrong_bound":
				metrics.DataAllocatedBytes = Bytes(plan.SafetyEnvelope.MaximumDataAllocatedBytes + 17)
				failure.Class, failure.Code = "resource", "data_allocated_ceiling"
				failure.Observation.Kind, failure.Observation.Metric = "gauge_limit", "data_allocated_bytes"
				failure.Observation.Limit, failure.Observation.Observed = plan.SafetyEnvelope.MaximumDataAllocatedBytes, uint64(metrics.DataAllocatedBytes)
				if mode == "wrong_primary" {
					failure.Observation.Observed++
					want = false
				}
				if mode == "wrong_bound" {
					failure.Observation.Limit--
					want = false
				}
			case "logical", "multiple":
				metrics.DataLogicalBytes = Bytes(plan.WorkEnvelope.MaximumDataLogicalBytes + 19)
				failure.Class, failure.Code = "resource", "data_logical_ceiling"
				failure.Observation.Kind, failure.Observation.Metric = "gauge_limit", "data_logical_bytes"
				failure.Observation.Limit, failure.Observation.Observed = plan.WorkEnvelope.MaximumDataLogicalBytes, uint64(metrics.DataLogicalBytes)
				if mode == "multiple" {
					metrics.DataAllocatedBytes = Bytes(plan.SafetyEnvelope.MaximumDataAllocatedBytes + 17)
					failure.Code, failure.Observation.Metric = "multiple_resource_ceilings", "multiple_resource_ceilings"
					failure.Observation.Limit, failure.Observation.Observed = 0, 1
				}
			case "topology":
				metrics.MaterializedOwnerPairs = 1
				metrics.DataLogicalBytes = Bytes(plan.WorkEnvelope.MaximumDataLogicalBytes + 19)
				failure.Class, failure.Code = "topology", "materialized_cartesian_owner_pairs_nonzero"
				failure.Observation.Kind, failure.Observation.Metric, failure.Observation.Observed = "counter_crossing", "materialized_cartesian_owner_pairs", 1
				priority = 1
			case "work":
				bound := plan.WorkEnvelope.Phases[index].ResolverBlobBytes.Maximum
				metrics.ResolverBlobBytes = Bytes(bound + 73)
				failure = workTestFailure(t, plan, phase, "resolver_blob_bytes", bound, uint64(metrics.ResolverBlobBytes), failure.Observation.UnavailableMetrics)
			case "wrong_available":
				metrics.AllocationMeasurementAvailable, want = true, false
			case "missing_coverage":
				failure.Observation.UnavailableMetrics, want = nil, false
			case "unknown":
				failure.Observation.UnavailableMetrics = append(failure.Observation.UnavailableMetrics, "unknown")
				want = false
			case "duplicate":
				failure.Observation.UnavailableMetrics = []string{"data_allocated_bytes", "data_allocated_bytes", "data_logical_bytes"}
				want = false
			case "passed":
				outcomes[phase], want = "passed", false
			}
			failure.Observation.EvidenceSHA256 = ""
			failure.Observation.EvidenceSHA256 = mustReceiptSHA256(t, failure.Observation)
			before := *metrics
			valid := validReceiptFailure(failure, phase, plan)
			err := validateReceiptMeasurements(values, outcomes, &failure, ReceiptTeardown{Outcome: "failed"}, 0, plan, freeze)
			if err == nil {
				err = validateStoppedFailureEvidence(Receipt{Measurements: values}, &failure, nil, plan, freeze)
			}
			_, actualPriority, decisionErr := expectedStoppedDecision(failure, values, plan)
			if (valid && err == nil && decisionErr == nil) != want {
				t.Fatalf("valid=%v measurements=%v decision=%v want=%v", valid, err, decisionErr, want)
			}
			if want && actualPriority != priority {
				t.Fatalf("priority=%d want=%d", actualPriority, priority)
			}
			if before != *metrics {
				t.Fatal("retained completed prefix changed")
			}
		})
	}
}

func TestByteReceiptTeardownPrefix(t *testing.T) {
	plan := accountingTestPlan(t)
	for _, overLimit := range []bool{false, true} {
		values, outcomes, freeze := failureCompositionMeasurements(plan, "product_queries")
		index := slices.Index(plan.PhaseOrder, "teardown")
		metrics := &values[index].Metrics
		metrics.DataAllocatedBytes, metrics.DataLogicalBytes = 23, 41
		if overLimit {
			metrics.DataAllocatedBytes = Bytes(plan.SafetyEnvelope.MaximumDataAllocatedBytes + 17)
			metrics.DataLogicalBytes = Bytes(plan.WorkEnvelope.MaximumDataLogicalBytes + 19)
		}
		prior := *metrics
		metrics.AllocationMeasurementAvailable = false
		teardown, _ := accountingTestTeardown(plan)
		teardown.Outcome, teardown.MeasurementErrors = "failed", 2
		teardown.MeasurementUnavailable = []string{"data_allocated_bytes", "data_logical_bytes"}
		teardown.Failure = &TeardownFailure{Schema: plan.ReceiptContract.TeardownFailureSchema, Kind: "multiple", FailedChecks: []string{"measurement_data_allocated_bytes_unavailable", "measurement_data_logical_bytes_unavailable"}}
		teardown.Failure.EvidenceSHA256 = mustReceiptSHA256(t, *teardown.Failure)
		if err := validateReceiptMeasurements(values, outcomes, nil, teardown, 0, plan, freeze); err != nil {
			t.Fatal(err)
		}
		if clean, err := validateReceiptTeardown(teardown, []PhaseMeasurement{values[index]}, plan, freeze, false); err != nil || clean {
			t.Fatal("exact failed teardown refused", clean, err)
		}
		teardown.Failure.FailedChecks = teardown.Failure.FailedChecks[:1]
		teardown.Failure.EvidenceSHA256 = ""
		teardown.Failure.EvidenceSHA256 = mustReceiptSHA256(t, *teardown.Failure)
		if _, err := validateReceiptTeardown(teardown, []PhaseMeasurement{values[index]}, plan, freeze, false); err == nil {
			t.Fatal("incorrect failed-check inventory admitted")
		}
		teardown.Outcome = "clean"
		if err := validateReceiptMeasurements(values, outcomes, nil, teardown, 0, plan, freeze); err == nil {
			t.Fatal("clean teardown accepted incomplete byte coverage")
		}
		if metrics.DataAllocatedBytes != prior.DataAllocatedBytes || metrics.DataLogicalBytes != prior.DataLogicalBytes {
			t.Fatal("teardown completed maximum changed")
		}
	}
}

func TestByteReceiptLegacyUnavailable(t *testing.T) {
	for _, path := range []string{"plan.json", "plan-v2.json"} {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var plan Plan
			if err := json.Unmarshal(raw, &plan); err != nil {
				t.Fatal(err)
			}
			phase := "product_queries"
			values, outcomes, freeze := failureCompositionMeasurements(plan, phase)
			for i := range values {
				if outcomes[values[i].Phase] == "not_run" {
					continue
				}
				values[i].DispatchAccounting, values[i].NativeObservation = nil, nil
				values[i].Metrics.DispatchMeasurementAvailable, values[i].Metrics.NativeMeasurementAvailable = false, false
				values[i].Metrics.ObservedRSSHighWaterBytes = 0
				values[i].Metrics.ProcessMeasurementAvailable, values[i].Metrics.PeakRSSBytes = true, 1024
				for _, role := range plan.WorkEnvelope.ChildProcessRoles {
					values[i].ChildProcessRoles = append(values[i].ChildProcessRoles, Count{Name: role})
				}
			}
			failure := ReceiptFailure{Phase: phase, Class: "internal", Code: "measurement_unavailable", Observation: FailureObservation{Kind: "measurement_unavailable", UnavailableMetrics: []string{"data_allocated_bytes", "data_logical_bytes"}}}
			metrics := &values[slices.Index(plan.PhaseOrder, phase)].Metrics
			metrics.AllocationMeasurementAvailable = false
			metrics.DataAllocatedBytes, metrics.DataLogicalBytes = 0, 0
			if err := validateReceiptMeasurements(values, outcomes, &failure, ReceiptTeardown{Outcome: "failed"}, 0, plan, freeze); err != nil {
				t.Fatal("legacy zero-unavailable control", err)
			}
			metrics.DataAllocatedBytes, metrics.DataLogicalBytes = 23, 41
			if err := validateReceiptMeasurements(values, outcomes, &failure, ReceiptTeardown{Outcome: "failed"}, 0, plan, freeze); err == nil {
				t.Fatal("legacy byte-prefix meaning changed")
			}
		})
	}
}

func TestByteReceiptPressureMaximum(t *testing.T) {
	for _, schema := range []string{PlanSchema, PlanV2Schema, PlanV3Schema, PlanV4Schema} {
		for _, phase := range []string{"pressure_80", "pressure_90", "pressure_75"} {
			value := PressureTransition{DataAllocatedBytesBefore: 20, DataAllocatedBytesAtTarget: 40, PrePressureAllocatedBytes: 10}
			if phase == "pressure_75" {
				value.DataAllocatedBytesBefore, value.DataAllocatedBytesAtTarget, value.RecoveryDataAllocatedBytes = 40, 30, 10
			}
			metrics := ReceiptMetrics{DataAllocatedBytes: 40}
			if !pressurePhaseAllocationMatches(value, metrics, phase, schema) {
				t.Fatal("existing maximum control", schema, phase)
			}
			metrics.DataAllocatedBytes++
			if pressurePhaseAllocationMatches(value, metrics, phase, schema) != processAccountingPlanSemantics(schema) {
				t.Fatal("earlier completed maximum", schema, phase)
			}
			for _, field := range []*uint64{&value.DataAllocatedBytesBefore, &value.DataAllocatedBytesAtTarget, &value.PrePressureAllocatedBytes, &value.RecoveryDataAllocatedBytes} {
				prior := *field
				*field = 42
				if processAccountingPlanSemantics(schema) && pressurePhaseAllocationMatches(value, metrics, phase, schema) {
					t.Fatal("retained endpoint exceeds maximum", phase)
				}
				*field = prior
			}
		}
	}
	if !pressureMutationMatches("add", 100, 120, 0, 20, 10, 30, 4) ||
		pressureMutationMatches("add", 100, 120, 0, 20, 10, 31, 4) ||
		pressureMutationMatches("add", 100, 125, 0, 20, 10, 30, 4) ||
		!pressureMutationMatches("remove", 120, 100, 20, 0, 30, 10, 4) {
		t.Fatal("existing pressure delta/tolerance predicates changed")
	}
}

func TestByteReceiptPressureSequence(t *testing.T) {
	for _, schema := range []string{PlanSchema, PlanV2Schema, PlanV3Schema, PlanV4Schema} {
		t.Run(schema, func(t *testing.T) {
			plan := accountingTestPlan(t)
			if schema == PlanV4Schema {
				plan = pressureContinuityTestPlan(t)
				if err := applyPressureContinuityCorrection(&plan); err != nil {
					t.Fatal(err)
				}
			} else if schema != PlanV3Schema {
				path := "plan.json"
				if schema == PlanV2Schema {
					path = "plan-v2.json"
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, &plan); err != nil {
					t.Fatal(err)
				}
			}
			// Only exact frozen geometry and epoch routing are needed here. Do not
			// run the unrelated full plan/profile admission constructor for this
			// bounded subvalidator fixture; these are explicitly unadmitted inputs.
			host := executionFreezeTestHost()
			pressure, err := expectedExecutionPressureGeometry(plan, host)
			if err != nil {
				t.Fatal(err)
			}
			epochs, epoch := correctedExecutionServerEpochs(), uint64(4)
			if schema == PlanSchema {
				epochs, epoch = frozenExecutionServerEpochs(), 2
			}
			freeze := ExecutionFreeze{Host: host, Pressure: pressure, Profile: ExecutionProfile{Epochs: epochs}}
			phases := []string{"pressure_80", "pressure_90", "pressure_75"}
			outcomes := make(map[string]string)
			authority := make(map[string]AuthorityPhaseResult)
			measurements := make([]PhaseMeasurement, len(phases))
			transitions := []TransitionResult{{Phase: "process_restart", Outcome: "passed", Injections: []InjectionTransition{{FailurePoint: "checkpointed_hard_restart", ProcessEpochAfter: epoch}}}}
			for index, phase := range phases {
				outcomes[phase] = "passed"
				authority[phase] = AuthorityPhaseResult{Phase: phase, Outcome: "passed", AuthorityState: AuthorityState{Current: true}}
				measurements[index].Phase = phase
				start := uint64(index+1) * 100
				transitions = append(transitions, TransitionResult{Phase: phase, Outcome: "passed", StartEventOrdinal: start, FinishEventOrdinal: start + 100})
			}
			// Existing modeled transition fixture; no filesystem or native admission
			// evidence is claimed. Its full pressure subvalidator remains in the path.
			testPressureTransitions(t, plan, freeze, authority, measurements, transitions)
			cloneSequence := func() ([]TransitionResult, map[string]ReceiptMetrics) {
				metrics := make(map[string]ReceiptMetrics, len(phases))
				cloned := slices.Clone(transitions)
				for index, phase := range phases {
					metrics[phase] = measurements[index].Metrics
					value := *transitions[index+1].Pressure
					cloned[index+1].Pressure = &value
				}
				return cloned, metrics
			}
			resequence := func(values []TransitionResult) {
				prior := SHA256([]byte("t422-pressure-sequence-start-v1"))
				for index, phase := range phases {
					value := values[index+1].Pressure
					value.PriorGateSequenceSHA256 = prior
					var err error
					value.GateSequenceSHA256, err = pressureSequenceSHA256(phase, *value)
					if err != nil {
						t.Fatal(err)
					}
					prior = value.GateSequenceSHA256
				}
			}
			for _, mode := range []string{"existing", "earlier_maximum", "endpoint_above_maximum", "wrong_delta", "one_byte_continuity", "outside_tolerance", "wrong_recovery", "wrong_pressure_schema"} {
				t.Run(mode, func(t *testing.T) {
					mutated, metrics := cloneSequence()
					if mode != "existing" {
						for _, phase := range phases {
							value := metrics[phase]
							value.DataAllocatedBytes += 100
							metrics[phase] = value
						}
					}
					switch mode {
					case "endpoint_above_maximum":
						value := metrics[phases[0]]
						value.DataAllocatedBytes = Bytes(mutated[1].Pressure.DataAllocatedBytesAtTarget - 1)
						metrics[phases[0]] = value
					case "wrong_delta":
						mutated[1].Pressure.DataAllocatedBytesAtTarget++
					case "one_byte_continuity":
						mutated[2].Pressure.DataAllocatedBytesBefore++
						mutated[2].Pressure.DataAllocatedBytesAtTarget++
					case "outside_tolerance":
						mutated[1].Pressure.VolumeUsedBytesAfter += freeze.Pressure.Targets[0].ToleranceBytes + 1
						mutated[1].Pressure.VolumeAvailableBytesAfter -= freeze.Pressure.Targets[0].ToleranceBytes + 1
					case "wrong_recovery":
						mutated[3].Pressure.RecoveryBallastAllocatedBytes = 1
					case "wrong_pressure_schema":
						mutated[1].Pressure.Schema = plan.ReceiptContract.TransitionSchema + "/pressure-v2"
						if schema == PlanV4Schema {
							mutated[1].Pressure.Schema = plan.ReceiptContract.TransitionSchema + "/pressure-v1"
						}
					}
					resequence(mutated)
					err := validatePressureTransitions(mutated, outcomes, authority, metrics, plan, freeze)
					want := mode == "existing" || mode == "earlier_maximum" && processAccountingPlanSemantics(schema) ||
						mode == "one_byte_continuity" && schema == PlanV4Schema
					if (err == nil) != want {
						t.Fatal(mode, err)
					}
				})
			}

			continuityCases := []struct {
				name  string
				drift uint64
				want  bool
			}{{"historical_exact", 1, false}}
			if schema == PlanV4Schema {
				continuityCases = []struct {
					name  string
					drift uint64
					want  bool
				}{
					{"boundary", InterphaseDriftToleranceBytes, true},
					{"boundary_plus_one", InterphaseDriftToleranceBytes + 1, false},
					{"persistent_release", 61_472_768, false},
				}
			}
			shift := func(value *uint64, amount uint64, increase bool) {
				if increase {
					*value += amount
				} else {
					*value -= amount
				}
			}
			setPressureMetric := func(metrics map[string]ReceiptMetrics, phase string, value *PressureTransition) {
				metric := metrics[phase]
				if processAccountingPlanSemantics(schema) {
					metric.DataAllocatedBytes = Bytes(max(uint64(metric.DataAllocatedBytes), value.DataAllocatedBytesBefore,
						value.DataAllocatedBytesAtTarget, value.PrePressureAllocatedBytes, value.RecoveryDataAllocatedBytes))
				} else if phase == "pressure_75" {
					metric.DataAllocatedBytes = Bytes(value.DataAllocatedBytesBefore)
				} else {
					metric.DataAllocatedBytes = Bytes(value.DataAllocatedBytesAtTarget)
				}
				metrics[phase] = metric
			}
			for _, term := range []string{"available", "allocated"} {
				for _, boundary := range []int{1, 2} {
					for _, increase := range []bool{false, true} {
						for _, test := range continuityCases {
							name := fmt.Sprintf("continuity_%s_%d_%t_%s", term, boundary, increase, test.name)
							t.Run(name, func(t *testing.T) {
								mutated, metrics := cloneSequence()
								value := mutated[boundary+1].Pressure
								if term == "available" {
									shift(&value.VolumeUsedBytesBefore, test.drift, increase)
									shift(&value.VolumeAvailableBytesBefore, test.drift, !increase)
									shift(&value.BallastAllocatedBytesAfter, test.drift, !increase)
									shift(&value.DataAllocatedBytesAtTarget, test.drift, !increase)
									setPressureMetric(metrics, phases[boundary], value)
								} else if boundary == 1 {
									shift(&value.DataAllocatedBytesBefore, test.drift, increase)
									shift(&value.DataAllocatedBytesAtTarget, test.drift, increase)
									setPressureMetric(metrics, phases[boundary], value)
								} else {
									precursor := uint64(0)
									if schema == PlanV4Schema {
										precursor = min(test.drift, InterphaseDriftToleranceBytes)
										prior := mutated[2].Pressure
										shift(&prior.DataAllocatedBytesBefore, precursor, !increase)
										shift(&prior.DataAllocatedBytesAtTarget, precursor, !increase)
										setPressureMetric(metrics, phases[1], prior)
									}
									residual := test.drift - precursor
									shift(&value.DataAllocatedBytesBefore, residual, increase)
									shift(&value.DataAllocatedBytesAtTarget, residual, increase)
									shift(&value.RecoveryDataAllocatedBytes, residual, increase)
									setPressureMetric(metrics, phases[boundary], value)
								}
								resequence(mutated)
								caseOutcomes := map[string]string{
									"pressure_80": "passed", "pressure_90": "passed", "pressure_75": "passed",
								}
								if boundary == 1 {
									caseOutcomes["pressure_75"] = "not_run"
								}
								err := validatePressureTransitions(mutated, caseOutcomes, authority, metrics, plan, freeze)
								if (err == nil) != test.want {
									t.Fatalf("drift=%d: %v", test.drift, err)
								}
								if !test.want {
									want := fmt.Sprintf("phase %q pressure capacity is not contiguous", phases[boundary])
									if err == nil || err.Error() != want {
										t.Fatalf("continuity refusal = %v, want %q", err, want)
									}
								}
							})
						}
					}
				}
			}

			recoveryCases := []struct {
				name  string
				drift uint64
				want  bool
			}{
				{"boundary", freeze.Pressure.Targets[2].ToleranceBytes, true},
				{"boundary_plus_one", freeze.Pressure.Targets[2].ToleranceBytes + 1, false},
			}
			if schema == PlanV4Schema {
				recoveryCases = []struct {
					name  string
					drift uint64
					want  bool
				}{
					{"boundary", InterphaseDriftToleranceBytes, true},
					{"boundary_plus_one", InterphaseDriftToleranceBytes + 1, false},
					{"persistent_release", 61_472_768, false},
				}
			}
			for _, largerDelta := range []bool{false, true} {
				for _, test := range recoveryCases {
					t.Run(fmt.Sprintf("recovery_%t_%s", largerDelta, test.name), func(t *testing.T) {
						mutated, metrics := cloneSequence()
						value := mutated[3].Pressure
						if largerDelta {
							value.RecoveryUsedBytes -= test.drift
							value.RecoveryAvailableBytes += test.drift
						} else {
							value.RecoveryUsedBytes += test.drift
							value.RecoveryAvailableBytes -= test.drift
						}
						value.RecoveryUsedPercent = usedPercentCeiling(value.RecoveryUsedBytes, freeze.Pressure.PressureVolumeBytes)
						resequence(mutated)
						err := validatePressureTransitions(mutated, outcomes, authority, metrics, plan, freeze)
						if (err == nil) != test.want {
							t.Fatalf("drift=%d: %v", test.drift, err)
						}
					})
				}
			}
		})
	}
}
