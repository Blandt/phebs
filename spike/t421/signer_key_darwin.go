//go:build darwin

package t421

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var executionSignerUmaskMu sync.Mutex

func generateExecutionSignerKey(ctx context.Context, key *executionSignerKeyCustody) error {
	if key == nil || key.claim == nil || key.signer == nil || key.namespace.owner == nil {
		return ErrExecutionEpochOne
	}
	key.signer.mu.Lock()
	defer key.signer.mu.Unlock()
	owner := key.namespace.owner
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if err := checkExecutionSignerKeyAuthoritiesLocked(ctx, key); err != nil {
		return err
	}
	for _, name := range key.claim.names.absentBeforeClaim() {
		path, err := executionSignerJoinedPath(owner.path, name, maxExecutionSignerPathBytes)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.New("T42.2 signer key destination changed after ceremony claim")
		}
	}
	privatePath := filepath.Join(owner.path, key.claim.names.temporaryKey)
	key.cleanupUncertain = true
	executionSignerUmaskMu.Lock()
	oldMask := syscall.Umask(0o077)
	stdout, stderr, runErr := runExecutionSignerCommandLocked(ctx, key, nil,
		"-q", "-t", "ed25519", "-N", "", "-C", "", "-f", privatePath)
	syscall.Umask(oldMask)
	executionSignerUmaskMu.Unlock()
	if runErr != nil || len(stdout) != 0 || len(stderr) != 0 {
		return errors.New("T42.2 signer key generation failed")
	}
	var err error
	key.temporaryPrivate, err = openExecutionSignerHeldFileLocked(ctx, owner, key.claim.names.temporaryKey)
	if err != nil {
		return err
	}
	key.temporaryPublic, err = openExecutionSignerHeldFileLocked(ctx, owner, key.claim.names.temporaryPublic)
	if err != nil {
		return err
	}
	key.cleanupUncertain = false
	if key.temporaryPrivate.file.Sync() != nil || key.temporaryPublic.file.Sync() != nil || owner.file.Sync() != nil {
		return errors.New("T42.2 signer temporary key durability failed")
	}
	stdout, stderr, runErr = runExecutionSignerCommandLocked(ctx, key,
		[]*executionSignerHeldFile{key.temporaryPrivate, key.temporaryPublic}, "-y", "-f", privatePath)
	if runErr != nil || len(stderr) != 0 {
		return errors.New("T42.2 signer public-key derivation failed")
	}
	canonical, fingerprint, publicSHA256, err := deriveExecutionSignerPublic(stdout)
	if err != nil {
		return err
	}
	generated, err := readExecutionSignerHeldFile(key.temporaryPublic)
	if err != nil || !bytes.Equal(generated, executionSignerGeneratedPublic(canonical)) {
		return errors.New("T42.2 generated and independently derived public keys differ")
	}
	canonicalName := "canonical-public-" + publicSHA256 + ".pub"
	fingerprintName := "fingerprint-" + publicSHA256 + ".claim.json"
	for _, name := range []string{canonicalName, fingerprintName} {
		path, pathErr := executionSignerJoinedPath(owner.path, name, maxExecutionSignerPathBytes)
		if pathErr != nil {
			return pathErr
		}
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			return errors.New("T42.2 signer public-key registry destination already exists or cannot be inspected")
		}
	}
	fingerprintRaw, err := MarshalCanonical(executionSignerFingerprintClaimV1{
		Schema: executionSignerFingerprintClaimSchema, SignerNamespaceSHA256: key.namespace.digest,
		CeremonyID: key.claim.ceremonyID, CanonicalPublicKey: string(canonical), SignerFingerprint: fingerprint,
	})
	if err != nil || len(fingerprintRaw) == 0 || len(fingerprintRaw) > maxExecutionSignerFingerprintClaimBytes {
		return errors.New("T42.2 signer fingerprint claim is not bounded canonical JSON")
	}
	key.fingerprintClaim, err = createExecutionSignerHeldFileLocked(ctx, owner, fingerprintName, fingerprintRaw,
		maxExecutionSignerFingerprintClaimBytes)
	if err != nil {
		return err
	}
	key.fingerprintRaw = append([]byte(nil), fingerprintRaw...)
	if err := promoteExecutionSignerFileLocked(ctx, owner, key.temporaryPrivate, key.claim.names.privateKey); err != nil {
		return err
	}
	key.privateKey, key.temporaryPrivate = key.temporaryPrivate, nil
	if err := promoteExecutionSignerFileLocked(ctx, owner, key.temporaryPublic, key.claim.names.generatedPublic); err != nil {
		return err
	}
	key.generatedPublic, key.temporaryPublic = key.temporaryPublic, nil
	key.canonicalPublic = append([]byte(nil), canonical...)
	key.publicSHA256, key.fingerprint = publicSHA256, fingerprint
	return checkExecutionSignerKeyLocked(ctx, key)
}

func runExecutionSignerCommandLocked(
	ctx context.Context,
	key *executionSignerKeyCustody,
	inputs []*executionSignerHeldFile,
	args ...string,
) ([]byte, []byte, error) {
	if err := checkExecutionSignerKeyAuthoritiesLocked(ctx, key); err != nil {
		return nil, nil, err
	}
	for _, input := range inputs {
		if err := checkExecutionSignerHeldFile(ctx, input); err != nil {
			return nil, nil, err
		}
	}
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stdout := checkoutCommandOutput{remaining: maxExecutionSignerKeyBytes, cancel: cancel}
	stderr := checkoutCommandOutput{remaining: 4 << 10, cancel: cancel}
	command := exec.CommandContext(commandCtx, key.signerPath, args...)
	command.Dir = key.namespace.owner.path
	command.Env = []string{
		"HOME=" + key.namespace.owner.path, "TMPDIR=" + key.namespace.owner.path,
		"TMP=" + key.namespace.owner.path, "TEMP=" + key.namespace.owner.path,
		"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "TZ=UTC",
	}
	command.Stdout, command.Stderr, command.WaitDelay = &stdout, &stderr, time.Second
	if err := runReferenceCommand(commandCtx, command); err != nil || commandCtx.Err() != nil || stdout.err != nil || stderr.err != nil {
		return nil, nil, ErrExecutionEpochOne
	}
	if err := checkExecutionSignerKeyAuthoritiesLocked(ctx, key); err != nil {
		return nil, nil, err
	}
	for _, input := range inputs {
		if err := checkExecutionSignerHeldFile(ctx, input); err != nil {
			return nil, nil, err
		}
	}
	return append([]byte(nil), stdout.buffer.Bytes()...), append([]byte(nil), stderr.buffer.Bytes()...), nil
}

func checkExecutionSignerKeyAuthoritiesLocked(ctx context.Context, key *executionSignerKeyCustody) error {
	identity, path, err := key.signer.checkLocked(ctx, "ssh-keygen")
	if err != nil || path != "/usr/bin/ssh-keygen" {
		return ErrExecutionEpochOne
	}
	if key.signerPath == "" {
		key.signerIdentity, key.signerPath = identity, path
	} else if key.signerIdentity != identity || key.signerPath != path {
		return ErrExecutionEpochOne
	}
	if err := checkExecutionSignerNamespaceLocked(ctx, key.namespace); err != nil {
		return err
	}
	if err := checkExecutionSignerCeremonyClaim(ctx, key.claim); err != nil {
		return err
	}
	return nil
}

func openExecutionSignerHeldFileLocked(ctx context.Context, owner *executionSignerNamespaceCustody, name string) (*executionSignerHeldFile, error) {
	path, err := executionSignerJoinedPath(owner.path, name, maxExecutionSignerPathBytes)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(owner.file.Fd()), name, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("T42.2 signer file cannot be held")
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("T42.2 signer file descriptor is unavailable")
	}
	held := &executionSignerHeldFile{file: file, path: path, name: name}
	held.identity, err = observeExecutionSignerFile(ctx, file, path, 1, maxExecutionSignerKeyBytes)
	if err != nil || held.identity.device != owner.identity.device {
		_ = file.Close()
		return nil, errors.New("T42.2 signer file identity is invalid")
	}
	return held, nil
}

func createExecutionSignerHeldFileLocked(
	ctx context.Context,
	owner *executionSignerNamespaceCustody,
	name string,
	raw []byte,
	maximum int,
) (*executionSignerHeldFile, error) {
	path, err := executionSignerJoinedPath(owner.path, name, maxExecutionSignerPathBytes)
	if err != nil || len(raw) < 1 || len(raw) > maximum {
		return nil, ErrExecutionEpochOne
	}
	fd, err := unix.Openat(int(owner.file.Fd()), name,
		unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, errors.New("T42.2 signer registry claim already exists or cannot be created")
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrExecutionEpochOne
	}
	held := &executionSignerHeldFile{file: file, path: path, name: name}
	if n, writeErr := file.Write(raw); writeErr != nil || n != len(raw) || file.Sync() != nil || owner.file.Sync() != nil {
		return held, errors.New("T42.2 signer registry claim write or durability failed")
	}
	held.identity, err = observeExecutionSignerFile(ctx, file, path, int64(len(raw)), int64(len(raw)))
	if err != nil || held.identity.device != owner.identity.device {
		return held, errors.New("T42.2 signer registry claim identity is invalid")
	}
	return held, nil
}

func promoteExecutionSignerFileLocked(
	ctx context.Context,
	owner *executionSignerNamespaceCustody,
	held *executionSignerHeldFile,
	destination string,
) error {
	if err := checkExecutionSignerHeldFile(ctx, held); err != nil || held.identity.device != owner.identity.device {
		return err
	}
	destinationPath, err := executionSignerJoinedPath(owner.path, destination, maxExecutionSignerPathBytes)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destinationPath); !errors.Is(err, os.ErrNotExist) {
		return errors.New("T42.2 signer final key already exists or cannot be inspected")
	}
	if err := unix.RenameatxNp(int(owner.file.Fd()), held.name, int(owner.file.Fd()), destination, unix.RENAME_EXCL); err != nil {
		return errors.New("T42.2 signer key promotion failed")
	}
	if owner.file.Sync() != nil {
		return errors.New("T42.2 signer key promotion durability failed")
	}
	if _, err := os.Lstat(held.path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("T42.2 signer key temporary path survived promotion")
	}
	held.name, held.path = destination, destinationPath
	if err := checkExecutionSignerHeldFile(ctx, held); err != nil || held.identity.device != owner.identity.device {
		return ErrExecutionEpochOne
	}
	return nil
}

func readExecutionSignerHeldFile(held *executionSignerHeldFile) ([]byte, error) {
	return readExecutionSignerFile(held, maxExecutionSignerKeyBytes)
}

func readExecutionSignerFile(held *executionSignerHeldFile, maximum int64) ([]byte, error) {
	if held == nil || held.file == nil {
		return nil, ErrExecutionEpochOne
	}
	if _, err := held.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(held.file, maximum+1))
	if err != nil || len(raw) < 1 || int64(len(raw)) > maximum {
		return nil, ErrExecutionEpochOne
	}
	return raw, nil
}

func checkExecutionSignerHeldFile(ctx context.Context, held *executionSignerHeldFile) error {
	if held == nil || held.file == nil {
		return ErrExecutionEpochOne
	}
	identity, err := observeExecutionSignerFile(ctx, held.file, held.path, held.identity.size, held.identity.size)
	if err != nil || identity != held.identity {
		return ErrExecutionEpochOne
	}
	return nil
}

func checkExecutionSignerKey(ctx context.Context, key *executionSignerKeyCustody) error {
	key.claim.mu.Lock()
	defer key.claim.mu.Unlock()
	key.signer.mu.Lock()
	defer key.signer.mu.Unlock()
	owner := key.namespace.owner
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return checkExecutionSignerKeyLocked(ctx, key)
}

func checkExecutionSignerKeyLocked(ctx context.Context, key *executionSignerKeyCustody) error {
	if len(key.canonicalPublic) == 0 || !validExecutionHexSHA256(key.publicSHA256) || !validSSHSHA256Fingerprint(key.fingerprint) ||
		key.privateKey == nil || key.generatedPublic == nil || key.fingerprintClaim == nil ||
		key.temporaryPrivate != nil || key.temporaryPublic != nil {
		return ErrExecutionEpochOne
	}
	if err := checkExecutionSignerKeyAuthoritiesLocked(ctx, key); err != nil {
		return err
	}
	for _, held := range []*executionSignerHeldFile{key.privateKey, key.generatedPublic, key.fingerprintClaim} {
		if err := checkExecutionSignerHeldFile(ctx, held); err != nil {
			return err
		}
	}
	generated, err := readExecutionSignerHeldFile(key.generatedPublic)
	if err != nil || !bytes.Equal(generated, executionSignerGeneratedPublic(key.canonicalPublic)) {
		return ErrExecutionEpochOne
	}
	fingerprintRaw, err := readExecutionSignerFile(key.fingerprintClaim, maxExecutionSignerFingerprintClaimBytes)
	if err != nil || !bytes.Equal(fingerprintRaw, key.fingerprintRaw) {
		return ErrExecutionEpochOne
	}
	canonical, fingerprint, publicSHA256, err := deriveExecutionSignerPublic(key.canonicalPublic)
	if err != nil || !bytes.Equal(canonical, key.canonicalPublic) || fingerprint != key.fingerprint || publicSHA256 != key.publicSHA256 {
		return ErrExecutionEpochOne
	}
	return nil
}

func closeExecutionSignerKey(key *executionSignerKeyCustody) error {
	if key.cleanupUncertain {
		return ErrExecutionEpochOne
	}
	owner := key.namespace.owner
	temporaries := []*executionSignerHeldFile{key.temporaryPrivate, key.temporaryPublic}
	hasTemporary := key.temporaryPrivate != nil || key.temporaryPublic != nil
	if hasTemporary {
		if owner == nil {
			return ErrExecutionEpochOne
		}
		owner.mu.Lock()
		defer owner.mu.Unlock()
		if err := checkExecutionSignerNamespaceLocked(context.Background(), key.namespace); err != nil {
			return err
		}
		for _, held := range temporaries {
			if held == nil {
				continue
			}
			if err := checkExecutionSignerHeldFile(context.Background(), held); err != nil {
				return ErrExecutionEpochOne
			}
		}
		for _, held := range temporaries {
			if held == nil {
				continue
			}
			if unix.Unlinkat(int(owner.file.Fd()), held.name, 0) != nil || owner.file.Sync() != nil {
				return ErrExecutionEpochOne
			}
			if _, err := os.Lstat(held.path); !errors.Is(err, os.ErrNotExist) {
				return ErrExecutionEpochOne
			}
		}
	}
	for _, held := range []*executionSignerHeldFile{
		key.temporaryPrivate, key.temporaryPublic, key.privateKey, key.generatedPublic, key.fingerprintClaim,
	} {
		if held != nil && held.file != nil && held.file.Close() != nil {
			return ErrExecutionEpochOne
		}
	}
	return nil
}
