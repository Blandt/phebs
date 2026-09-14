package t421

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// Modeled package authentication isolates framing; it is not signed evidence.
func returnedTransportTestBinding(raw []byte) ReturnedPackageBinding {
	return ReturnedPackageBinding{
		packageSHA256: SHA256(raw), signerFingerprint: executionFreezeTestSigner(),
		returnedSignatureVerified: true, sourceSignatureVerified: true, sourceVerified: true,
	}
}

func TestExecutionReturnedPackageFrame(t *testing.T) {
	raw := []byte("modeled authenticated package bytes")
	bound := returnedTransportTestBinding(raw)
	frame, err := frameExecutionReturnedPackage(raw, bound)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func([]byte) []byte
		valid  bool
	}{
		{"exact", func(b []byte) []byte { return b }, true},
		{"missing", func([]byte) []byte { return nil }, false},
		{"magic", func(b []byte) []byte { b[0] ^= 1; return b }, false},
		{"short_header", func(b []byte) []byte { return b[:11] }, false},
		{"empty", func(b []byte) []byte { binary.BigEndian.PutUint32(b[8:], 0); return b }, false},
		{"overflow", func(b []byte) []byte { binary.BigEndian.PutUint32(b[8:], ^uint32(0)); return b[:12] }, false},
		{"truncated", func(b []byte) []byte { return b[:len(b)-1] }, false},
		{"trailing", func(b []byte) []byte { return append(b, 0) }, false},
		{"duplicate", func(b []byte) []byte { return append(b, b...) }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := captureExecutionReturnedPackage(bytes.NewReader(test.change(bytes.Clone(frame))))
			if (err == nil) != test.valid || test.valid && !bytes.Equal(got, raw) || !test.valid && got != nil {
				t.Fatalf("capture validity = %v, want %v", err, test.valid)
			}
		})
	}
	for _, change := range []func(*ReturnedPackageBinding){
		func(b *ReturnedPackageBinding) { b.packageSHA256 = SHA256([]byte("different")) },
		func(b *ReturnedPackageBinding) { b.returnedSignatureVerified = false },
		func(b *ReturnedPackageBinding) { b.sourceSignatureVerified = false },
		func(b *ReturnedPackageBinding) { b.sourceVerified = false },
		func(b *ReturnedPackageBinding) { b.signerFingerprint = "" },
	} {
		changed := bound
		change(&changed)
		if got, err := frameExecutionReturnedPackage(raw, changed); err == nil || got != nil {
			t.Fatal("unauthenticated bytes were framed")
		}
	}
	if _, err := captureExecutionReturnedPackage(nil); err == nil {
		t.Fatal("nil reader accepted")
	}
}

func TestExecutionReturnedPackageCaptureRequiresSuccessfulEOF(t *testing.T) {
	raw := []byte("modeled package")
	frame, err := frameExecutionReturnedPackage(raw, returnedTransportTestBinding(raw))
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := writer.Write(frame)
		if err == nil {
			err = writer.CloseWithError(io.ErrUnexpectedEOF)
		}
		done <- err
	}()
	got, err := captureExecutionReturnedPackage(reader)
	_ = reader.Close()
	if writeErr := <-done; writeErr != nil {
		t.Fatal(writeErr)
	}
	if err == nil || got != nil {
		t.Fatal("errored end-of-stream accepted")
	}
}
