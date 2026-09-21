package t421

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func pressureContinuityTestPlan(t *testing.T) Plan {
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

func TestPressureContinuityV4DerivationIsNarrow(t *testing.T) {
	prior := pressureContinuityTestPlan(t)
	priorRaw, err := MarshalCanonical(prior)
	if err != nil {
		t.Fatal(err)
	}
	next := prior
	if err := applyPressureContinuityCorrection(&next); err != nil {
		t.Fatal(err)
	}
	if next.Schema != PlanV4Schema || next.ToolPolicy.ExecutionFreezeSchema != ExecutionFreezeV4Schema ||
		next.ReceiptContract.Schema != ReceiptV4Schema ||
		strings.Count(next.MeterPolicy.LifecycleSemantics, pressureContinuityPolicyV4) != 1 {
		t.Fatal("V4 pressure-continuity bindings are incomplete")
	}
	restored := next
	restored.Schema = prior.Schema
	restored.ToolPolicy.ExecutionFreezeSchema = prior.ToolPolicy.ExecutionFreezeSchema
	restored.ReceiptContract.Schema = prior.ReceiptContract.Schema
	restored.MeterPolicy.LifecycleSemantics = prior.MeterPolicy.LifecycleSemantics
	if !reflect.DeepEqual(restored, prior) {
		t.Fatal("V4 derivation changed an unrelated plan field")
	}
	if err := validatePlanExecutionContract(next); err != nil {
		t.Fatal(err)
	}
	raw, err := MarshalCanonical(next)
	if err != nil || bytes.Count(raw, []byte{'\n'}) != 1 {
		t.Fatal("V4 plan is not compact canonical", err)
	}
	again, err := MarshalCanonical(prior)
	if err != nil || !bytes.Equal(again, priorRaw) {
		t.Fatal("V4 derivation changed V3 canonical bytes", err)
	}
}

func TestPressureContinuityV4RequiresCompleteV3(t *testing.T) {
	if err := applyPressureContinuityCorrection(nil); err == nil {
		t.Fatal("nil plan acquired V4 authority")
	}
	incomplete := accountingTestPlan(t)
	if err := applyPressureContinuityCorrection(&incomplete); err == nil {
		t.Fatal("incomplete V3 plan acquired V4 authority")
	}
	for _, test := range []struct {
		name   string
		mutate func(*Plan)
	}{
		{"schema", func(plan *Plan) { plan.Schema = PlanV2Schema }},
		{"freeze", func(plan *Plan) { plan.ToolPolicy.ExecutionFreezeSchema = ExecutionFreezeSchema }},
		{"receipt", func(plan *Plan) { plan.ReceiptContract.Schema = ReceiptSchema }},
		{"accounting", func(plan *Plan) { plan.ProcessAccounting = nil }},
		{"logical", func(plan *Plan) { plan.LogicalStoreWork = nil }},
		{"selector", func(plan *Plan) { plan.SelectorHandoffCleanup = nil }},
		{"policy", func(plan *Plan) { plan.MeterPolicy.LifecycleSemantics += ";changed" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := pressureContinuityTestPlan(t)
			test.mutate(&plan)
			if err := applyPressureContinuityCorrection(&plan); err == nil {
				t.Fatal("mutated V3 plan acquired V4 authority")
			}
		})
	}
	plan := pressureContinuityTestPlan(t)
	if err := applyPressureContinuityCorrection(&plan); err != nil {
		t.Fatal(err)
	}
	if err := applyPressureContinuityCorrection(&plan); err == nil {
		t.Fatal("V4 correction was applied twice")
	}
}

func TestPressureContinuityV4GeometryAndHistoricalOmission(t *testing.T) {
	for _, plan := range lifecyclePolicyPlans(t) {
		geometry, err := expectedExecutionPressureGeometry(plan, executionFreezeTestHost())
		raw, marshalErr := json.Marshal(geometry)
		if err != nil || marshalErr != nil || geometry.InterphaseDriftToleranceBytes != 0 ||
			bytes.Contains(raw, []byte("interphase_drift_tolerance_bytes")) {
			t.Fatalf("historical geometry changed for %s: %v / %v", plan.Schema, err, marshalErr)
		}
	}
	plan := pressureContinuityTestPlan(t)
	if err := applyPressureContinuityCorrection(&plan); err != nil {
		t.Fatal(err)
	}
	geometry, err := expectedExecutionPressureGeometry(plan, executionFreezeTestHost())
	if err != nil {
		t.Fatal(err)
	}
	wantPolicy := "collect_noncurrent_no_padding_then_require_150_second_sampled_nonballast_anchor_stability_before_first_target-v3;" + pressureContinuityPolicyV4
	raw, err := json.Marshal(geometry)
	if err != nil || geometry.InterphaseDriftToleranceBytes != InterphaseDriftToleranceBytes ||
		geometry.LivePrePressurePolicy != wantPolicy ||
		!bytes.Contains(raw, []byte(`"interphase_drift_tolerance_bytes":65536`)) {
		t.Fatal("V4 pressure geometry omitted its signed continuity bound", err)
	}
}

func TestPressureContinuityV4CanonicalArtifactRouting(t *testing.T) {
	for _, value := range []any{
		Plan{Schema: PlanV4Schema}, &Plan{Schema: PlanV4Schema},
		ExecutionFreeze{Schema: ExecutionFreezeV4Schema}, &ExecutionFreeze{Schema: ExecutionFreezeV4Schema},
		Receipt{Schema: ReceiptV4Schema}, &Receipt{Schema: ReceiptV4Schema},
	} {
		raw, err := MarshalCanonical(value)
		if err != nil || bytes.Count(raw, []byte{'\n'}) != 1 {
			t.Fatalf("V4 artifact is not compact canonical: %T / %v", value, err)
		}
	}
}

func TestPressureContinuityV4FullFrozenRoundTrip(t *testing.T) {
	plan, err := BuildPlanV4(testSourceCommit)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := MarshalCanonical(plan)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePlan(raw)
	if err != nil || !reflect.DeepEqual(decoded, plan) {
		t.Fatal("V4 full frozen round trip failed", err)
	}
}

func TestPressureContinuityCanonicalFreezeVersioning(t *testing.T) {
	plans := lifecyclePolicyPlans(t)
	plans = plans[:2]
	v3 := pressureContinuityTestPlan(t)
	v4 := v3
	if err := applyPressureContinuityCorrection(&v4); err != nil {
		t.Fatal(err)
	}
	plans = append(plans, v3, v4)
	want := map[string][2]string{
		PlanSchema:   {retainedPlanSHA256, "sha256:ba06a05af75bd48595052ff7c76a340f9d85007e91b35688b8b6f990ef472e1c"},
		PlanV2Schema: {retainedPlanV2SHA256, "sha256:b46ba67cfeb036e8a961837448c8e8cff3d3a892b369a4289cd21adf0db2600e"},
		PlanV3Schema: {"sha256:f7533949a5304a9cf1a30f9d4cf89b0da1f7cb6ef90273f723445b3791569744", "sha256:711f996e47a09cac5c7774bc3d5bd607dcca920d105d74d1c020e4c2b96f11aa"},
		PlanV4Schema: {"sha256:0e1b8ec95fecfbbe5e5ed810bd2fdac311fd5f12d94deb6db88cf5535c0203a2", "sha256:e52be77a8ad6216a69f5947b48cc36f0e3b163cdc7a66584d19b7f921d8d50e8"},
	}
	for _, plan := range plans {
		t.Run(plan.Schema, func(t *testing.T) {
			planRaw, err := MarshalCanonical(plan)
			if err != nil {
				t.Fatal(err)
			}
			commits := executionFreezeTestCommits()
			tools := executionFreezeTestTools(plan, commits)
			host := executionFreezeTestHost()
			admission := executionProfileTestAdmission(t, plan, tools, host)
			checkout := executionFreezeTestCheckout(t, commits, tools)
			freeze, err := BuildExecutionFreeze(plan, commits, tools, host, executionFreezeTestSigner(), checkout, admission)
			if err != nil {
				t.Fatal(err)
			}
			freezeRaw, err := MarshalCanonical(freeze)
			if err != nil {
				t.Fatal(err)
			}
			if got := [2]string{SHA256(planRaw), SHA256(freezeRaw)}; got != want[plan.Schema] {
				t.Fatalf("canonical plan/freeze digests = %q", got)
			}
			containsTolerance := bytes.Contains(freezeRaw, []byte("interphase_drift_tolerance_bytes"))
			if containsTolerance != (plan.Schema == PlanV4Schema) {
				t.Fatal("pressure drift tolerance field has wrong version presence")
			}
			decoded, err := DecodeExecutionFreeze(
				freezeRaw, plan, commits, executionFreezeTestSigner(), checkout, admission,
			)
			if err != nil || !reflect.DeepEqual(decoded, freeze) {
				t.Fatal("canonical freeze round trip failed", err)
			}
		})
	}
}

func TestPressureContinuityV4CandidateFreezeRoundTrip(t *testing.T) {
	plan := pressureContinuityTestPlan(t)
	if err := applyPressureContinuityCorrection(&plan); err != nil {
		t.Fatal(err)
	}
	fixture := newExecutionFreezeCandidateTestFixtureForPlan(t, plan)
	raw, err := fixture.assemble()
	if err != nil {
		t.Fatal(err)
	}
	freeze, err := fixture.validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := MarshalCanonical(freeze)
	if err != nil || freeze.Schema != ExecutionFreezeV4Schema ||
		freeze.Pressure.InterphaseDriftToleranceBytes != InterphaseDriftToleranceBytes ||
		!bytes.Equal(raw, again) {
		t.Fatal("V4 candidate freeze did not round-trip through private revalidation", err)
	}
}
