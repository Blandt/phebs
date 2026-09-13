package t421

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
)

const (
	executionSignerFingerprintClaimSchema   = "t422-signer-fingerprint-claim-v1"
	maxExecutionSignerKeyBytes              = 1 << 10
	maxExecutionSignerFingerprintClaimBytes = 4 << 10
)

type executionSignerFingerprintClaimV1 struct {
	Schema                string `json:"schema"`
	SignerNamespaceSHA256 string `json:"signer_namespace_sha256"`
	CeremonyID            string `json:"ceremony_id"`
	CanonicalPublicKey    string `json:"canonical_public_key"`
	SignerFingerprint     string `json:"signer_fingerprint"`
}

type executionSignerHeldFile struct {
	file     *os.File
	path     string
	name     string
	identity executionSignerFileIdentity
}

// executionSignerKeyCustody owns one generated Ed25519 pair plus the retained
// fingerprint claim. It carries no candidate, signature, authorization,
// checkout, ordinal, or handoff authority.
type executionSignerKeyCustody struct {
	mu               sync.Mutex
	claim            *executionSignerCeremonyClaimCustody
	namespace        executionSignerNamespaceBinding
	signer           *ExecutionSystemToolCustody
	signerIdentity   ExecutionToolIdentity
	signerPath       string
	temporaryPrivate *executionSignerHeldFile
	temporaryPublic  *executionSignerHeldFile
	privateKey       *executionSignerHeldFile
	generatedPublic  *executionSignerHeldFile
	fingerprintClaim *executionSignerHeldFile
	fingerprintRaw   []byte
	canonicalPublic  []byte
	publicSHA256     string
	fingerprint      string
	cleanupUncertain bool
	closed           bool
}

// prepareExecutionSignerKey spends the claim's sole key-preparation attempt
// before running either signer child. Failures after this point remain bound
// to the already durable ceremony claim and cannot retry the ID.
func prepareExecutionSignerKey(
	ctx context.Context,
	claim *executionSignerCeremonyClaimCustody,
	signer *ExecutionSystemToolCustody,
) (*executionSignerKeyCustody, error) {
	if claim == nil || signer == nil {
		return nil, ErrExecutionEpochOne
	}
	claim.mu.Lock()
	defer claim.mu.Unlock()
	if ctx == nil || claim.closed || claim.file == nil || claim.keyUsed {
		return nil, ErrExecutionEpochOne
	}
	claim.keyUsed = true
	if ctx.Err() != nil || checkExecutionSignerCeremonyClaim(ctx, claim) != nil {
		return nil, ErrExecutionEpochOne
	}
	namespace, err := claim.namespace.recheck(ctx)
	if err != nil {
		return nil, ErrExecutionEpochOne
	}
	key := &executionSignerKeyCustody{claim: claim, namespace: namespace, signer: signer}
	if err := generateExecutionSignerKey(ctx, key); err != nil {
		return key, err
	}
	return key, nil
}

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

func executionSignerGeneratedPublic(canonical []byte) []byte {
	if len(canonical) == 0 || canonical[len(canonical)-1] != '\n' {
		return nil
	}
	value := make([]byte, 0, len(canonical)+1)
	value = append(value, canonical[:len(canonical)-1]...)
	value = append(value, ' ', '\n')
	return value
}

func (key *executionSignerKeyCustody) check(ctx context.Context) error {
	if key == nil || ctx == nil || ctx.Err() != nil {
		return ErrExecutionEpochOne
	}
	key.mu.Lock()
	defer key.mu.Unlock()
	if key.closed {
		return ErrExecutionEpochOne
	}
	return checkExecutionSignerKey(ctx, key)
}

func (key *executionSignerKeyCustody) Close() error {
	if key == nil {
		return nil
	}
	key.mu.Lock()
	defer key.mu.Unlock()
	if key.closed {
		return nil
	}
	if err := closeExecutionSignerKey(key); err != nil {
		return err
	}
	key.closed = true
	return nil
}
