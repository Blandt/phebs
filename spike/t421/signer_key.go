package t421

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	executionSignerFingerprintClaimSchema   = "t422-signer-fingerprint-claim-v1"
	maxExecutionSignerKeyBytes              = 1 << 10
	maxExecutionSignerFingerprintClaimBytes = 4 << 10
)

func deriveExecutionSignerPublic(raw []byte) (canonical []byte, fingerprint, publicSHA256 string, err error) {
	if len(raw) < 1 || len(raw) > maxExecutionSignerKeyBytes || raw[len(raw)-1] != '\n' ||
		strings.Count(string(raw), "\n") != 1 || strings.ContainsAny(string(raw), "\r\x00\t") {
		return nil, "", "", errors.New("T42.2 derived signer public key is not one canonical line")
	}
	line := string(raw[:len(raw)-1])
	if !strings.HasPrefix(line, "ssh-ed25519 ") || strings.Count(line, " ") != 1 {
		return nil, "", "", errors.New("T42.2 derived signer public key is not an Ed25519 key")
	}
	encoded := strings.TrimPrefix(line, "ssh-ed25519 ")
	blob, decodeErr := base64.StdEncoding.Strict().DecodeString(encoded)
	if decodeErr != nil || base64.StdEncoding.EncodeToString(blob) != encoded || !validExecutionEd25519Blob(blob) {
		return nil, "", "", errors.New("T42.2 derived signer public key has invalid RFC4253 bytes")
	}
	fingerprintDigest := sha256.Sum256(blob)
	publicDigest := sha256.Sum256(raw)
	return append([]byte(nil), raw...),
		"SHA256:" + base64.RawStdEncoding.EncodeToString(fingerprintDigest[:]),
		hex.EncodeToString(publicDigest[:]), nil
}

func validExecutionEd25519Blob(raw []byte) bool {
	algorithm, rest, ok := executionSignerSSHString(raw)
	if !ok || string(algorithm) != "ssh-ed25519" {
		return false
	}
	key, rest, ok := executionSignerSSHString(rest)
	return ok && len(key) == 32 && len(rest) == 0
}

func executionSignerSSHString(raw []byte) ([]byte, []byte, bool) {
	if len(raw) < 4 {
		return nil, nil, false
	}
	size := binary.BigEndian.Uint32(raw[:4])
	if uint64(size) > uint64(len(raw)-4) {
		return nil, nil, false
	}
	end := 4 + int(size)
	return raw[4:end], raw[end:], true
}
