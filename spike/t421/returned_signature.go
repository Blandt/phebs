package t421

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/pem"

	"golang.org/x/crypto/ssh"
)

// verifyExecutionReturnedSignature verifies the OpenSSH SSHSIG v1 envelope
// (openssh-portable/PROTOCOL.sshsig), using the already installed SSH verifier.
// The expected public key comes from the authenticated freeze, never the
// signature's embedded key. This performs no signing or operational admission.
func verifyExecutionReturnedSignature(raw, message, expectedPublic []byte, namespace string) error {
	if len(raw) == 0 || len(raw) > maxExecutionSignerSignatureBytes || namespace == "" ||
		!bytes.HasPrefix(raw, []byte("-----BEGIN SSH SIGNATURE-----\n")) {
		return ErrExecutionLauncher
	}
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "SSH SIGNATURE" || len(block.Headers) != 0 || len(rest) != 0 ||
		len(block.Bytes) < 10 || string(block.Bytes[:6]) != "SSHSIG" || binary.BigEndian.Uint32(block.Bytes[6:10]) != 1 {
		return ErrExecutionLauncher
	}
	var envelope struct {
		PublicKey     []byte
		Namespace     string
		Reserved      string
		HashAlgorithm string
		Signature     []byte
	}
	if ssh.Unmarshal(block.Bytes[10:], &envelope) != nil || envelope.Namespace != namespace || envelope.Reserved != "" {
		return ErrExecutionLauncher
	}
	canonical, _, _, err := deriveExecutionSignerPublic(expectedPublic)
	if err != nil {
		return ErrExecutionLauncher
	}
	key, _, _, tail, err := ssh.ParseAuthorizedKey(canonical)
	if err != nil || len(tail) != 0 || key.Type() != ssh.KeyAlgoED25519 || !bytes.Equal(key.Marshal(), envelope.PublicKey) {
		return ErrExecutionLauncher
	}
	var digest []byte
	switch envelope.HashAlgorithm {
	case "sha256":
		value := sha256.Sum256(message)
		digest = value[:]
	case "sha512":
		value := sha512.Sum512(message)
		digest = value[:]
	default:
		return ErrExecutionLauncher
	}
	var signature ssh.Signature
	if ssh.Unmarshal(envelope.Signature, &signature) != nil || signature.Format != ssh.KeyAlgoED25519 || len(signature.Rest) != 0 {
		return ErrExecutionLauncher
	}
	signed := append([]byte("SSHSIG"), ssh.Marshal(struct {
		Namespace     string
		Reserved      string
		HashAlgorithm string
		Digest        []byte
	}{envelope.Namespace, envelope.Reserved, envelope.HashAlgorithm, digest})...)
	if key.Verify(signed, &signature) != nil {
		return ErrExecutionLauncher
	}
	return nil
}
