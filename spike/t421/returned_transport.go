package t421

import (
	"bytes"
	"encoding/binary"
	"io"
)

const executionReturnedFrameMagic = "T422PKG1"

// frameExecutionReturnedPackage accepts only bytes from the authenticated
// package builder. The framing is transport, never an admission capability.
func frameExecutionReturnedPackage(raw []byte, binding ReturnedPackageBinding) ([]byte, error) {
	if len(raw) == 0 || uint64(len(raw)) > frozenSealPolicy().MaximumPackageBytes ||
		!binding.returnedSignatureVerified || !binding.sourceSignatureVerified || !binding.sourceVerified ||
		!validSSHSHA256Fingerprint(binding.signerFingerprint) || binding.packageSHA256 != SHA256(raw) {
		return nil, ErrExecutionLauncher
	}
	frame := make([]byte, len(executionReturnedFrameMagic)+4+len(raw))
	copy(frame, executionReturnedFrameMagic)
	binary.BigEndian.PutUint32(frame[len(executionReturnedFrameMagic):], uint32(len(raw)))
	copy(frame[len(executionReturnedFrameMagic)+4:], raw)
	return frame, nil
}

// captureExecutionReturnedPackage returns untrusted bytes only after one exact
// bounded frame and EOF. The caller must authenticate them before publication.
// Its reader must have the enclosing launcher's cancellation/close lifetime.
func captureExecutionReturnedPackage(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, ErrExecutionLauncher
	}
	var header [len(executionReturnedFrameMagic) + 4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil ||
		!bytes.Equal(header[:len(executionReturnedFrameMagic)], []byte(executionReturnedFrameMagic)) {
		return nil, ErrExecutionLauncher
	}
	size := binary.BigEndian.Uint32(header[len(executionReturnedFrameMagic):])
	if size == 0 || uint64(size) > frozenSealPolicy().MaximumPackageBytes {
		return nil, ErrExecutionLauncher
	}
	raw := make([]byte, int(size))
	if _, err := io.ReadFull(reader, raw); err != nil {
		return nil, ErrExecutionLauncher
	}
	var sentinel [1]byte
	if n, err := io.ReadFull(reader, sentinel[:]); n != 0 || err != io.EOF {
		return nil, ErrExecutionLauncher
	}
	return raw, nil
}
