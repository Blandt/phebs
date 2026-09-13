//go:build darwin

package t421

import (
	"reflect"
	"testing"
)

func TestBuildExecutionReturnedPackageAuthenticatesExactInventoryOnce(t *testing.T) {
	plan := clonePlan(t, correctedTestPlan(t))
	if err := applyProcessAccountingCorrection(&plan); err != nil {
		t.Fatal(err)
	}
	commits := executionFreezeTestCommits()
	tools, host := executionFreezeTestTools(plan, commits), executionFreezeTestHost()
	namespace := newExecutionSignerNamespaceTestBinding(t)
	profileAdmission := executionProfileTestAdmission(t, plan, tools, host, namespace.digest)
	profile, err := expectedExecutionProfile(plan, tools, host, profileAdmission)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := claimExecutionSignerCeremony(t.Context(), namespace, "t422-returned-package", canonicalSignerClaimTestRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = claim.Close() })
	signer, err := HoldExecutionSystemTool(t.Context(), "ssh-keygen")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = signer.Close() })
	key, err := prepareExecutionSignerKey(t.Context(), claim, signer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = key.Close() })
	raw, err := assembleExecutionFreezeCandidate(plan, commits, tools, host, key.fingerprint, namespace, profile, profileAdmission)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := sealExecutionFreezeCandidate(t.Context(), key, plan, executionFreezeCandidatePreparation{
		raw: raw, commits: commits, checkout: executionFreezeTestCheckout(t, commits, tools),
		profile: profile, profileAdmission: profileAdmission, namespace: namespace,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = seal.Close() })
	admission, err := seal.verifyAndIssueAdmission(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	freezeBinding, err := bindExecutionFreezeForReceipt(seal.freeze, plan, commits, key.fingerprint, namespace.digest, admission)
	if err != nil {
		t.Fatal(err)
	}
	receipt := completeTestReceipt(t, plan, freezeBinding)
	sourceVerification, err := executionSourceVerificationBytes(plan, freezeBinding, receipt.RevisionResults)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Authority.SourceVerificationSHA256 = SHA256(sourceVerification)

	packageRaw, packageBinding, err := buildExecutionReturnedPackage(t.Context(), plan, receipt, freezeBinding, seal)
	if err != nil {
		t.Fatal(err)
	}
	files, err := inspectExecutionReturnedPackage(packageRaw, plan)
	if err != nil || len(files) != 11 || packageBinding.packageSHA256 != SHA256(packageRaw) ||
		!reflect.DeepEqual(packageBinding.exactInventory, plan.SealPolicy.ExactInventory) {
		t.Fatal("returned package did not retain the exact authenticated V3 inventory", err)
	}
	if _, _, err := buildExecutionReturnedPackage(t.Context(), plan, receipt, freezeBinding, seal); err == nil {
		t.Fatal("returned-package authority was reusable")
	}
}
