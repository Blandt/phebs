package t421

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	maxExecutionSignerSignatureBytes    = 4 << 10
	maxExecutionReturnedControlBytes    = 4 << 10
	maxExecutionSourceVerificationBytes = MaxReceiptBytes
)

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
