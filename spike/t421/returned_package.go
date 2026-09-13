package t421

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

const (
	maxExecutionReturnedControlBytes    = 4 << 10
	maxExecutionSourceVerificationBytes = MaxReceiptBytes
)

type executionSourceVerificationV1 struct {
	Schema                string                                `json:"schema"`
	PlanSHA256            string                                `json:"plan_sha256"`
	ExecutionFreezeSHA256 string                                `json:"execution_freeze_sha256"`
	RevisionResultsSHA256 string                                `json:"revision_results_sha256"`
	ExactInventorySHA256  string                                `json:"exact_inventory_sha256"`
	Revisions             []executionSourceVerificationRevision `json:"revisions"`
	SourceFree            bool                                  `json:"source_free"`
}

type executionSourceVerificationRevision struct {
	Name                      string               `json:"name"`
	BaseCommit                string               `json:"base_commit"`
	BaseTree                  string               `json:"base_tree"`
	TreeInventory             SetIdentity          `json:"tree_inventory"`
	TreeOID                   string               `json:"tree_oid"`
	ObservationInputInventory SetIdentity          `json:"observation_input_inventory"`
	CandidateInventories      []CandidateInventory `json:"candidate_inventories"`
	TypedInputKind            string               `json:"typed_input_kind"`
	TypedInputPath            string               `json:"typed_input_path"`
	TypedInputMode            string               `json:"typed_input_mode"`
	TypedInputBytes           uint64               `json:"typed_input_bytes"`
	TypedInputSHA256          string               `json:"typed_input_sha256"`
	TypedInputBlobOID         string               `json:"typed_input_blob_oid"`
	RawCommitSHA256           string               `json:"raw_commit_sha256"`
	CommitOID                 string               `json:"commit_oid"`
}

type executionReturnedManifestV1 struct {
	Schema                string `json:"schema"`
	PlanSHA256            string `json:"plan_sha256"`
	ExecutionFreezeSHA256 string `json:"execution_freeze_sha256"`
	ResultsSHA256         string `json:"results_sha256"`
	SignerFingerprint     string `json:"signer_fingerprint"`
	SourceFree            bool   `json:"source_free"`
}

type executionReturnedEntry struct {
	name    string
	maximum int64
}

var executionReturnedEntries = []executionReturnedEntry{
	{"SHA256SUMS", maxExecutionReturnedControlBytes},
	{"SHA256SUMS.sig", maxExecutionSignerSignatureBytes},
	{"allowed_signers", maxExecutionSignerKeyBytes},
	{"execution-freeze.json", MaxExecutionFreezeBytes},
	{"execution-freeze.json.sig", maxExecutionSignerSignatureBytes},
	{"manifest.json", maxExecutionReturnedControlBytes},
	{"plan.json", MaxPlanBytes},
	{"results.json", MaxReceiptBytes},
	{"signer.pub", maxExecutionSignerKeyBytes},
	{"source-verification.json", maxExecutionSourceVerificationBytes},
	{"source-verification.json.sig", maxExecutionSignerSignatureBytes},
}

// buildExecutionReturnedPackage spends the sealed freeze's sole package
// attempt. It returns bytes only after the complete archive and Receipt have
// been independently decoded and authenticated.
func buildExecutionReturnedPackage(
	ctx context.Context,
	plan Plan,
	receipt Receipt,
	binding ExecutionFreezeBinding,
	seal *executionSignerSealCustody,
) ([]byte, ReturnedPackageBinding, error) {
	if seal == nil {
		return nil, ReturnedPackageBinding{}, ErrExecutionEpochOne
	}
	seal.mu.Lock()
	defer seal.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || seal.closed || seal.packageUsed || seal.key == nil || plan.Schema != PlanV3Schema {
		return nil, ReturnedPackageBinding{}, ErrExecutionEpochOne
	}
	seal.packageUsed = true
	return createExecutionReturnedPackage(ctx, plan, receipt, binding, seal)
}

func executionSourceVerificationBytes(plan Plan, binding ExecutionFreezeBinding, revisions []RevisionResult) ([]byte, error) {
	if plan.Schema != PlanV3Schema || len(revisions) != len(plan.Revisions.Physical) {
		return nil, ErrExecutionEpochOne
	}
	rows := make([]executionSourceVerificationRevision, len(revisions))
	for index, result := range revisions {
		physical, ok := namedPhysicalRevision(plan.Revisions.Physical, result.Name)
		if !ok || physical.Name != result.Name {
			return nil, ErrExecutionEpochOne
		}
		manifest := result.AuthoredManifest
		rows[index] = executionSourceVerificationRevision{
			Name: result.Name, BaseCommit: physical.BaseCommit, BaseTree: physical.BaseTree,
			TreeInventory: manifest.TreeInventory, TreeOID: result.PhysicalTree,
			ObservationInputInventory: physical.ExpectedObservationInputInventory,
			CandidateInventories:      slices.Clone(physical.ExpectedCandidateInventories),
			TypedInputKind:            manifest.TypedInputKind, TypedInputPath: manifest.TypedInputPath,
			TypedInputMode: manifest.TypedInputMode, TypedInputBytes: manifest.TypedInputBytes,
			TypedInputSHA256: manifest.TypedInputSHA256, TypedInputBlobOID: manifest.TypedInputBlobOID,
			RawCommitSHA256: manifest.CommitBytesSHA256, CommitOID: result.PhysicalCommit,
		}
	}
	planSHA256, err := receiptSHA256(plan)
	if err != nil || binding.planSHA256 != planSHA256 || !validDigest(binding.freezeSHA256) {
		return nil, ErrExecutionEpochOne
	}
	revisionSHA256, err := receiptSHA256(revisions)
	if err != nil {
		return nil, err
	}
	inventorySHA256, err := receiptSHA256(plan.SealPolicy.ExactInventory)
	if err != nil {
		return nil, err
	}
	raw, err := MarshalCanonical(executionSourceVerificationV1{
		Schema: plan.SealPolicy.SourceVerificationSchema, PlanSHA256: planSHA256,
		ExecutionFreezeSHA256: binding.freezeSHA256, RevisionResultsSHA256: revisionSHA256,
		ExactInventorySHA256: inventorySHA256, Revisions: rows, SourceFree: true,
	})
	if err != nil || len(raw) == 0 || len(raw) > maxExecutionSourceVerificationBytes {
		return nil, ErrExecutionEpochOne
	}
	return raw, nil
}

func validateExecutionSourceVerification(raw []byte, plan Plan, receipt Receipt, binding ExecutionFreezeBinding) error {
	want, err := executionSourceVerificationBytes(plan, binding, receipt.RevisionResults)
	if err != nil || !bytes.Equal(raw, want) || receipt.Authority.SourceVerificationSHA256 != SHA256(raw) {
		return errors.New("T42.2 source verification differs from exact receipt authority")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value executionSourceVerificationV1
	if decoder.Decode(&value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("T42.2 source verification is not one canonical value")
	}
	return nil
}

func executionReturnedChecksums(files map[string][]byte, coverage []string) ([]byte, error) {
	var result strings.Builder
	for _, name := range coverage {
		raw, ok := files[name]
		if !ok || len(raw) == 0 {
			return nil, ErrExecutionEpochOne
		}
		digest := strings.TrimPrefix(SHA256(raw), "sha256:")
		fmt.Fprintf(&result, "%s  %s\n", digest, name)
	}
	return []byte(result.String()), nil
}

func marshalExecutionReturnedPackage(files map[string][]byte, plan Plan) ([]byte, error) {
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	gzipWriter.ModTime = time.Time{}
	gzipWriter.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	var expanded uint64
	for _, name := range plan.SealPolicy.ExactInventory {
		raw, ok := files[name]
		if !ok || len(raw) == 0 {
			return nil, ErrExecutionEpochOne
		}
		expanded += uint64(len(raw))
		if expanded > plan.SealPolicy.MaximumExpandedBytes {
			return nil, errors.New("T42.2 returned package exceeds its expanded-byte bound")
		}
		header := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(raw)), ModTime: time.Unix(0, 0), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}
		if err := tarWriter.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := tarWriter.Write(raw); err != nil {
			return nil, err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	if compressed.Len() == 0 || uint64(compressed.Len()) > plan.SealPolicy.MaximumPackageBytes {
		return nil, errors.New("T42.2 returned package exceeds its transfer bound")
	}
	return slices.Clone(compressed.Bytes()), nil
}

func inspectExecutionReturnedPackage(raw []byte, plan Plan) (map[string][]byte, error) {
	if len(raw) == 0 || uint64(len(raw)) > plan.SealPolicy.MaximumPackageBytes {
		return nil, ErrExecutionEpochOne
	}
	compressed := bytes.NewReader(raw)
	gzipReader, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, err
	}
	if !gzipReader.ModTime.IsZero() || gzipReader.OS != 255 ||
		gzipReader.Name != "" || gzipReader.Comment != "" || len(gzipReader.Extra) != 0 {
		return nil, errors.New("T42.2 returned package gzip header is invalid")
	}
	gzipReader.Multistream(false)
	limited := &io.LimitedReader{R: gzipReader, N: int64(plan.SealPolicy.MaximumExpandedBytes) + 1}
	tarReader := tar.NewReader(limited)
	wanted := make(map[string]int64, len(executionReturnedEntries))
	for _, entry := range executionReturnedEntries {
		wanted[entry.name] = entry.maximum
	}
	files := make(map[string][]byte, len(wanted))
	index := 0
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nil, fmt.Errorf("T42.2 returned package header: %w", nextErr)
		}
		maximum, ok := wanted[header.Name]
		if !ok || index >= len(plan.SealPolicy.ExactInventory) || header.Name != plan.SealPolicy.ExactInventory[index] ||
			header.Typeflag != tar.TypeReg || header.Linkname != "" || header.Uname != "" || header.Gname != "" ||
			header.Mode != 0o600 || header.Uid != 0 || header.Gid != 0 || header.Size < 1 || header.Size > maximum ||
			!header.ModTime.Equal(time.Unix(0, 0)) || header.Format != tar.FormatUSTAR || files[header.Name] != nil {
			return nil, errors.New("T42.2 returned package header is invalid")
		}
		content, readErr := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if readErr != nil || int64(len(content)) != header.Size {
			return nil, errors.New("T42.2 returned package entry is truncated")
		}
		files[header.Name] = content
		index++
	}
	tail, tailErr := io.ReadAll(limited)
	for _, value := range tail {
		if value != 0 {
			return nil, errors.New("T42.2 returned package has data after its end marker")
		}
	}
	if tailErr != nil || int64(plan.SealPolicy.MaximumExpandedBytes)+1-limited.N > int64(plan.SealPolicy.MaximumExpandedBytes) ||
		gzipReader.Close() != nil || compressed.Len() != 0 || len(files) != len(wanted) || index != len(plan.SealPolicy.ExactInventory) {
		return nil, errors.New("T42.2 returned package is incomplete or has trailing data")
	}
	for _, name := range plan.SealPolicy.ExactInventory {
		if files[name] == nil {
			return nil, errors.New("T42.2 returned package inventory is incomplete")
		}
	}
	return files, nil
}

func executionReturnedBinding(plan Plan, receipt Receipt, binding ExecutionFreezeBinding, packageRaw, checksums, source []byte, signer string) (ReturnedPackageBinding, error) {
	receiptDigest, err := receiptSHA256(receipt)
	if err != nil {
		return ReturnedPackageBinding{}, err
	}
	revisionSHA256, err := receiptSHA256(receipt.RevisionResults)
	if err != nil {
		return ReturnedPackageBinding{}, err
	}
	exactInventorySHA256, err := receiptSHA256(plan.SealPolicy.ExactInventory)
	if err != nil {
		return ReturnedPackageBinding{}, err
	}
	return ReturnedPackageBinding{
		signerFingerprint: signer, receiptSHA256: receiptDigest, packageSHA256: SHA256(packageRaw),
		inventorySHA256: SHA256(checksums), exactInventory: slices.Clone(plan.SealPolicy.ExactInventory),
		returnedSignatureVerified: true, sourceSignatureVerified: true,
		returnedSignatureNamespace: plan.SealPolicy.ReturnedSignatureNamespace,
		sourceSignatureNamespace:   plan.SealPolicy.SourceVerificationSignatureNamespace,
		sourceVerificationSHA256:   SHA256(source), sourceVerificationSchema: plan.SealPolicy.SourceVerificationSchema,
		sourcePlanSHA256: binding.planSHA256, sourceFreezeSHA256: binding.freezeSHA256,
		sourceExactInventorySHA256: exactInventorySHA256, revisionResultsSHA256: revisionSHA256, sourceVerified: true,
	}, nil
}

func validExecutionReturnedChecksum(raw []byte, files map[string][]byte, coverage []string) bool {
	want, err := executionReturnedChecksums(files, coverage)
	if err != nil || !bytes.Equal(raw, want) {
		return false
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if len(line) < 67 || line[64:66] != "  " {
			return false
		}
		decoded, err := hex.DecodeString(line[:64])
		if err != nil || hex.EncodeToString(decoded) != line[:64] {
			return false
		}
	}
	return true
}
