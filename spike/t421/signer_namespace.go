package t421

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
)

const (
	executionSignerNamespaceSchema   = "t422-signer-namespace-binding-v1"
	maxExecutionSignerNamespaceBytes = 8 << 10
)

type signerNamespaceBindingPreimageV1 struct {
	Schema                  string `json:"schema"`
	SignerControlRoot       string `json:"signer_control_root"`
	SignerControlRootDevice int64  `json:"signer_control_root_st_dev"`
	SignerControlRootInode  uint64 `json:"signer_control_root_st_ino"`
	SignerControlRootMode   uint32 `json:"signer_control_root_st_mode"`
}

type executionSignerNamespaceIdentity struct {
	device int64
	inode  uint64
	mode   uint32
	uid    uint32
}

// executionSignerNamespaceCustody retains the exact selected registry root.
// Its digest binds the canonical path and held inode identity without making a
// machine-, user-, or cross-root uniqueness claim.
type executionSignerNamespaceCustody struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	identity executionSignerNamespaceIdentity
	digest   string
	token    *byte
}

// executionSignerNamespaceBinding is produced only by a live custody recheck.
// It is intentionally private and carries no signing or registry authority.
type executionSignerNamespaceBinding struct {
	owner  *executionSignerNamespaceCustody
	token  *byte
	digest string
}

func holdExecutionSignerNamespace(ctx context.Context, path string) (*executionSignerNamespaceCustody, error) {
	if ctx == nil || ctx.Err() != nil || !validExecutionLauncherPath(path) {
		return nil, ErrExecutionEpochOne
	}
	file, err := openExecutionSignerNamespace(path)
	if err != nil {
		return nil, ErrExecutionEpochOne
	}
	token := byte(1)
	custody := &executionSignerNamespaceCustody{file: file, path: path, token: &token}
	identity, err := observeExecutionSignerNamespace(ctx, file, path)
	if err != nil {
		_ = file.Close()
		return nil, ErrExecutionEpochOne
	}
	digest, err := executionSignerNamespaceSHA256(path, identity)
	if err != nil {
		_ = file.Close()
		return nil, ErrExecutionEpochOne
	}
	custody.identity, custody.digest = identity, digest
	return custody, nil
}

func (custody *executionSignerNamespaceCustody) check(ctx context.Context) (executionSignerNamespaceBinding, error) {
	if custody == nil || ctx == nil || ctx.Err() != nil {
		return executionSignerNamespaceBinding{}, ErrExecutionEpochOne
	}
	custody.mu.Lock()
	defer custody.mu.Unlock()
	if custody.file == nil || custody.token == nil {
		return executionSignerNamespaceBinding{}, ErrExecutionEpochOne
	}
	identity, err := observeExecutionSignerNamespace(ctx, custody.file, custody.path)
	if err != nil || identity != custody.identity {
		return executionSignerNamespaceBinding{}, ErrExecutionEpochOne
	}
	digest, err := executionSignerNamespaceSHA256(custody.path, identity)
	if err != nil || digest != custody.digest {
		return executionSignerNamespaceBinding{}, ErrExecutionEpochOne
	}
	return executionSignerNamespaceBinding{owner: custody, token: custody.token, digest: digest}, nil
}

func (binding executionSignerNamespaceBinding) valid() bool {
	if binding.owner == nil || binding.token == nil || !validExecutionHexSHA256(binding.digest) {
		return false
	}
	binding.owner.mu.Lock()
	defer binding.owner.mu.Unlock()
	return binding.owner.file != nil && binding.owner.token == binding.token && binding.owner.digest == binding.digest
}

func (binding executionSignerNamespaceBinding) recheck(ctx context.Context) (executionSignerNamespaceBinding, error) {
	if !binding.valid() {
		return executionSignerNamespaceBinding{}, ErrExecutionEpochOne
	}
	current, err := binding.owner.check(ctx)
	if err != nil || current.owner != binding.owner || current.token != binding.token || current.digest != binding.digest {
		return executionSignerNamespaceBinding{}, ErrExecutionEpochOne
	}
	return current, nil
}

func (custody *executionSignerNamespaceCustody) Close() error {
	if custody == nil {
		return nil
	}
	custody.mu.Lock()
	defer custody.mu.Unlock()
	if custody.file == nil {
		return nil
	}
	file := custody.file
	custody.file, custody.token = nil, nil
	return file.Close()
}

func executionSignerNamespaceSHA256(path string, identity executionSignerNamespaceIdentity) (string, error) {
	preimage := signerNamespaceBindingPreimageV1{
		Schema: executionSignerNamespaceSchema, SignerControlRoot: path,
		SignerControlRootDevice: identity.device, SignerControlRootInode: identity.inode,
		SignerControlRootMode: identity.mode,
	}
	raw, err := MarshalCanonical(preimage)
	if err != nil || len(raw) == 0 || len(raw) > maxExecutionSignerNamespaceBytes {
		return "", errors.New("T42.2 signer namespace preimage is invalid")
	}
	digest := strings.TrimPrefix(SHA256(raw), "sha256:")
	if !validExecutionHexSHA256(digest) {
		return "", errors.New("T42.2 signer namespace digest is invalid")
	}
	return digest, nil
}
