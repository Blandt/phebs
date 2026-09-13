package t421

import (
	"bytes"
	"compress/gzip"
	"testing"
)

func TestInspectExecutionReturnedPackageRejectsMalformedHeader(t *testing.T) {
	for _, data := range [][]byte{[]byte("truncated tar header"), bytes.Repeat([]byte{'x'}, 512)} {
		var raw bytes.Buffer
		writer := gzip.NewWriter(&raw)
		writer.OS = 255
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		plan := Plan{SealPolicy: SealPolicy{MaximumPackageBytes: 4096, MaximumExpandedBytes: 4096}}
		if files, err := inspectExecutionReturnedPackage(raw.Bytes(), plan); err == nil || files != nil {
			t.Fatal("malformed tar header did not fail closed")
		}
	}
}
