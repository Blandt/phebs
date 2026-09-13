//go:build darwin

package t421

import (
	"bytes"
	"context"
	"errors"
	"reflect"
)

func createExecutionReturnedPackage(
	ctx context.Context,
	plan Plan,
	receipt Receipt,
	binding ExecutionFreezeBinding,
	seal *executionSignerSealCustody,
) ([]byte, ReturnedPackageBinding, error) {
	key := seal.key
	key.mu.Lock()
	defer key.mu.Unlock()
	key.claim.mu.Lock()
	defer key.claim.mu.Unlock()
	key.signer.mu.Lock()
	defer key.signer.mu.Unlock()
	owner := key.namespace.owner
	owner.mu.Lock()
	defer owner.mu.Unlock()
	refuse := func(err error) ([]byte, ReturnedPackageBinding, error) { return nil, ReturnedPackageBinding{}, err }
	if err := checkExecutionSignerSealLocked(ctx, seal); err != nil ||
		!reflect.DeepEqual(binding.freeze, seal.freeze) || binding.freezeSHA256 != SHA256(seal.candidateRaw) ||
		binding.expectedSignerFingerprint != key.fingerprint || binding.expectedSignerNamespaceSHA256 != key.namespace.digest ||
		binding.admissionEventOrdinal != 1 {
		return refuse(ErrExecutionEpochOne)
	}
	if err := verifyExecutionSignerCandidateLocked(ctx, key, seal.candidate, seal.signature, seal.candidateRaw); err != nil {
		return refuse(err)
	}
	planRaw, err := MarshalCanonical(plan)
	if err != nil {
		return refuse(err)
	}
	if decoded, decodeErr := DecodePlan(planRaw); decodeErr != nil || !reflect.DeepEqual(decoded, plan) {
		return refuse(ErrExecutionEpochOne)
	}
	sourceRaw, err := executionSourceVerificationBytes(plan, binding, receipt.RevisionResults)
	if err != nil || receipt.Authority.SourceVerificationSHA256 != SHA256(sourceRaw) {
		return refuse(errors.New("T42.2 receipt does not bind its source verification"))
	}
	seal.sourceSignature, err = signExecutionReturnedPayloadLocked(ctx, key, sourceRaw,
		plan.SealPolicy.SourceVerificationSignatureNamespace, key.claim.names.sourceSignatureStage, key.claim.names.sourceSignature)
	if err != nil {
		return refuse(err)
	}
	sourceSignatureRaw, err := readExecutionSignerFile(seal.sourceSignature, maxExecutionSignerSignatureBytes)
	if err != nil {
		return refuse(err)
	}
	receiptRaw, err := MarshalCanonical(receipt)
	if err != nil || len(receiptRaw) == 0 || len(receiptRaw) > MaxReceiptBytes || rejectSourceBearingReceipt(receiptRaw) != nil {
		return refuse(ErrExecutionEpochOne)
	}
	manifestRaw, err := MarshalCanonical(executionReturnedManifestV1{
		Schema: plan.SealPolicy.ManifestSchema, PlanSHA256: binding.planSHA256,
		ExecutionFreezeSHA256: binding.freezeSHA256, ResultsSHA256: SHA256(receiptRaw),
		SignerFingerprint: key.fingerprint, SourceFree: true,
	})
	if err != nil || len(manifestRaw) > maxExecutionReturnedControlBytes {
		return refuse(ErrExecutionEpochOne)
	}
	freezeSignatureRaw, err := readExecutionSignerFile(seal.signature, maxExecutionSignerSignatureBytes)
	if err != nil {
		return refuse(err)
	}
	files := map[string][]byte{
		"allowed_signers": executionSignerAllowlist(key.canonicalPublic), "execution-freeze.json": seal.candidateRaw,
		"execution-freeze.json.sig": freezeSignatureRaw, "manifest.json": manifestRaw,
		"plan.json": planRaw, "results.json": receiptRaw, "signer.pub": key.canonicalPublic,
		"source-verification.json": sourceRaw, "source-verification.json.sig": sourceSignatureRaw,
	}
	checksums, err := executionReturnedChecksums(files, plan.SealPolicy.ChecksumCoverage)
	if err != nil || len(checksums) > maxExecutionReturnedControlBytes {
		return refuse(ErrExecutionEpochOne)
	}
	seal.returnedSignature, err = signExecutionReturnedPayloadLocked(ctx, key, checksums,
		plan.SealPolicy.ReturnedSignatureNamespace, key.claim.names.returnedSignatureStage, key.claim.names.returnedSignature)
	if err != nil {
		return refuse(err)
	}
	returnedSignatureRaw, err := readExecutionSignerFile(seal.returnedSignature, maxExecutionSignerSignatureBytes)
	if err != nil {
		return refuse(err)
	}
	files["SHA256SUMS"], files["SHA256SUMS.sig"] = checksums, returnedSignatureRaw
	packageRaw, err := marshalExecutionReturnedPackage(files, plan)
	if err != nil {
		return refuse(err)
	}
	verified, err := inspectExecutionReturnedPackage(packageRaw, plan)
	if err != nil || !reflect.DeepEqual(files, verified) ||
		!validExecutionReturnedChecksum(verified["SHA256SUMS"], verified, plan.SealPolicy.ChecksumCoverage) {
		return refuse(ErrExecutionEpochOne)
	}
	if err := validateExecutionSourceVerification(verified["source-verification.json"], plan, receipt, binding); err != nil ||
		!bytes.Equal(verified["source-verification.json.sig"], sourceSignatureRaw) ||
		verifyExecutionSignerPayloadLocked(ctx, key, seal.sourceSignature, verified["source-verification.json"], plan.SealPolicy.SourceVerificationSignatureNamespace) != nil ||
		!bytes.Equal(verified["SHA256SUMS.sig"], returnedSignatureRaw) ||
		verifyExecutionSignerPayloadLocked(ctx, key, seal.returnedSignature, verified["SHA256SUMS"], plan.SealPolicy.ReturnedSignatureNamespace) != nil {
		return refuse(ErrExecutionEpochOne)
	}
	packageBinding, err := executionReturnedBinding(plan, receipt, binding, packageRaw, checksums, sourceRaw, key.fingerprint)
	if err != nil {
		return refuse(err)
	}
	decodedReceipt, err := DecodeReceipt(verified["results.json"], plan, binding, packageBinding)
	if err != nil || !reflect.DeepEqual(decodedReceipt, receipt) {
		return refuse(ErrExecutionEpochOne)
	}
	return packageRaw, packageBinding, nil
}

func signExecutionReturnedPayloadLocked(
	ctx context.Context,
	key *executionSignerKeyCustody,
	raw []byte,
	namespace, stageName, finalName string,
) (*executionSignerHeldFile, error) {
	stdout, _, err := runExecutionSignerPayloadCommandLocked(ctx, key,
		[]*executionSignerHeldFile{key.privateKey, key.canonicalFile, key.allowlist}, raw,
		maxExecutionSignerSignatureBytes, "-Y", "sign", "-f", key.privateKey.path, "-n", namespace)
	if err != nil || len(stdout) == 0 || len(stdout) > maxExecutionSignerSignatureBytes {
		return nil, ErrExecutionEpochOne
	}
	stage, err := createExecutionSignerHeldFileLocked(ctx, key.namespace.owner, stageName, stdout, maxExecutionSignerSignatureBytes)
	if err != nil {
		return stage, err
	}
	if err := verifyExecutionSignerPayloadLocked(ctx, key, stage, raw, namespace); err != nil {
		return stage, err
	}
	if err := promoteExecutionSignerFileLocked(ctx, key.namespace.owner, stage, finalName); err != nil {
		return stage, err
	}
	return stage, nil
}
