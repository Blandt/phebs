// Package archiveevidence observes existing archive reads. It owns no filesystem
// access, restore authority, or additional archive traversal.
package archiveevidence

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"math"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const Schema = "phebs-archive-artifact-inventory-v1"

type Stage uint8

const (
	Before Stage = iota + 1
	Archived
	After
)

type Identity struct {
	Records     uint64 `json:"records"`
	FramedBytes uint64 `json:"framed_bytes"`
	SHA256      string `json:"sha256"`
}

// Observation is source-free. After means exact native artifact readback, or
// successful committed native database payloads; it never means a live DB dump.
type Observation struct {
	Stage    Stage
	Path     string
	Identity Identity
}

type observerKey struct{}
type inventoryKey struct{}
type captureKey struct{}

var ErrObservation = errors.New("archive artifact observation unavailable")

func WithObserver(ctx context.Context, observe func(Observation) error) (context.Context, error) {
	if ctx == nil || observe == nil || ctx.Value(observerKey{}) != nil {
		return nil, ErrObservation
	}
	return context.WithValue(ctx, observerKey{}, observe), nil
}

func Selected(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Value(observerKey{}).(func(Observation) error)
	return ok
}

// WithoutObserver excludes an existing disposable verification round trip from
// the actual archive-to-installation observation scope.
func WithoutObserver(ctx context.Context) context.Context {
	if !Selected(ctx) {
		return ctx
	}
	return context.WithValue(ctx, observerKey{}, struct{}{})
}

func Emit(ctx context.Context, value Observation) (err error) {
	if !Selected(ctx) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	defer func() {
		if recover() != nil {
			err = ErrObservation
		}
	}()
	if value.Stage < Before || value.Stage > After || !safePath(value.Path) || !ValidIdentity(value.Identity) {
		return ErrObservation
	}
	if err := ctx.Value(observerKey{}).(func(Observation) error)(value); err != nil {
		return err
	}
	return ctx.Err()
}

func ValidIdentity(value Identity) bool {
	if value.Records > math.MaxUint64/8 || value.FramedBytes < value.Records*8 || !validDigest(value.SHA256) {
		return false
	}
	if value.Records == 0 {
		return value == (&Stream{}).Identity()
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(value[7:])
	return err == nil && strings.ToLower(value) == value
}

type Record struct {
	Path   string `json:"path"`
	Bytes  uint64 `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Stream frames an already ordered record sequence without retaining payloads.
// Database units use increasing fixed-width ordinal paths, not SQL text.
type Stream struct {
	digest   hash.Hash
	identity Identity
	prior    string
}

func (stream *Stream) Add(record Record) error {
	if !safePath(record.Path) || record.Path <= stream.prior ||
		!validDigest(record.SHA256) {
		return ErrObservation
	}
	raw, err := json.Marshal(record)
	if err != nil || stream.identity.FramedBytes > math.MaxUint64-8 || uint64(len(raw)) > math.MaxUint64-8-stream.identity.FramedBytes || stream.identity.Records == math.MaxUint64 {
		return ErrObservation
	}
	if stream.digest == nil {
		stream.digest = sha256.New()
	}
	var frame [8]byte
	binary.BigEndian.PutUint64(frame[:], uint64(len(raw)))
	_, _ = stream.digest.Write(frame[:])
	_, _ = stream.digest.Write(raw)
	stream.identity.Records++
	stream.identity.FramedBytes += uint64(len(raw)) + 8
	stream.prior = record.Path
	return nil
}

func (stream *Stream) Identity() Identity {
	value := stream.identity
	if stream.digest == nil {
		sum := sha256.Sum256(nil)
		value.SHA256 = "sha256:" + hex.EncodeToString(sum[:])
	} else {
		value.SHA256 = "sha256:" + hex.EncodeToString(stream.digest.Sum(nil))
	}
	return value
}

type entry struct {
	sum      [32]byte
	bytes    uint64
	verified bool
}

// Inventory retains one digest per existing admitted archive path. Verification
// reuses that inventory: an actual successful native read must match the digest
// before it marks an entry. No copied extraction digest alone can close After.
type Inventory struct {
	component string
	maximum   int
	entries   map[string]entry
	root      string
	verifying bool
	err       error
}

func New(ctx context.Context, component string, maximum int) *Inventory {
	if !Selected(ctx) {
		return nil
	}
	return &Inventory{component: component, maximum: maximum, entries: make(map[string]entry)}
}

func (inventory *Inventory) CaptureContext(ctx context.Context) context.Context {
	if inventory == nil {
		return ctx
	}
	return context.WithValue(ctx, captureKey{}, inventory)
}

func Capturing(ctx context.Context) *Inventory {
	if ctx == nil {
		return nil
	}
	value, _ := ctx.Value(captureKey{}).(*Inventory)
	return value
}

func Reading(ctx context.Context) bool {
	return ctx != nil && ctx.Value(inventoryKey{}) != nil
}

func (inventory *Inventory) Add(name string, size uint64, sum [32]byte) error {
	if inventory == nil {
		return nil
	}
	if inventory.err != nil {
		return inventory.err
	}
	if !safePath(name) || inventory.verifying || inventory.maximum <= 0 || len(inventory.entries) >= inventory.maximum {
		inventory.err = ErrObservation
		return inventory.err
	}
	if _, exists := inventory.entries[name]; exists {
		inventory.err = ErrObservation
		return inventory.err
	}
	inventory.entries[name] = entry{sum: sum, bytes: size}
	return nil
}

// CopyN replaces an existing native copy; it adds hashing only when selected.
// The owner still checks descriptor identity, sync, close and complete native
// validation before emitting any complete observation.
func (inventory *Inventory) CopyN(destination io.Writer, source io.Reader, size int64, name string) (int64, error) {
	if inventory == nil {
		return io.CopyN(destination, source, size)
	}
	digest := sha256.New()
	written, err := io.CopyN(io.MultiWriter(destination, digest), source, size)
	if err != nil {
		return written, err
	}
	var sum [32]byte
	_ = digest.Sum(sum[:0])
	return written, inventory.Add(name, uint64(written), sum)
}

func (inventory *Inventory) VerificationContext(ctx context.Context, root string) (context.Context, error) {
	if inventory == nil {
		return ctx, nil
	}
	if inventory.err != nil || inventory.verifying || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, ErrObservation
	}
	inventory.verifying, inventory.root = true, root
	return context.WithValue(ctx, inventoryKey{}, inventory), nil
}

// ObserveRead is called only after a complete stable native file read succeeds.
// Reads of synthesized restore artifacts are outside the declared archived set;
// all original archive paths still require an independent matching read.
func ObserveRead(ctx context.Context, filename string, raw []byte) error {
	if ctx == nil {
		return nil
	}
	inventory, _ := ctx.Value(inventoryKey{}).(*Inventory)
	if inventory == nil {
		return nil
	}
	return inventory.observe(filename, uint64(len(raw)), sha256.Sum256(raw))
}

func ObserveDigest(ctx context.Context, filename string, size uint64, sum [32]byte) error {
	if ctx == nil {
		return nil
	}
	inventory, _ := ctx.Value(inventoryKey{}).(*Inventory)
	if inventory == nil {
		return nil
	}
	return inventory.observe(filename, size, sum)
}

type trackedReader struct {
	source io.Reader
	digest hash.Hash
	bytes  uint64
}

func (reader *trackedReader) Read(raw []byte) (int, error) {
	n, err := reader.source.Read(raw)
	_, _ = reader.digest.Write(raw[:n])
	reader.bytes += uint64(n)
	return n, err
}

// TrackRead wraps an already-owned native read without opening or seeking. Call
// finish only after that reader's complete content and identity checks succeed.
// A missing observation scope returns the exact original reader and nil finish.
func TrackRead(ctx context.Context, filename string, source io.Reader) (io.Reader, func() error) {
	if ctx == nil {
		return source, nil
	}
	inventory, _ := ctx.Value(inventoryKey{}).(*Inventory)
	if inventory == nil {
		return source, nil
	}
	reader := &trackedReader{source: source, digest: sha256.New()}
	return reader, func() error {
		var sum [32]byte
		_ = reader.digest.Sum(sum[:0])
		return inventory.observe(filename, reader.bytes, sum)
	}
}

func (inventory *Inventory) observe(filename string, size uint64, sum [32]byte) error {
	if inventory.err != nil {
		return inventory.err
	}
	relative, err := filepath.Rel(inventory.root, filename)
	if err != nil || !safePath(filepath.ToSlash(relative)) {
		inventory.err = ErrObservation
		return inventory.err
	}
	name := filepath.ToSlash(relative)
	want, exists := inventory.entries[name]
	if !exists {
		return nil
	}
	if !inventory.verifying || want.bytes != size || want.sum != sum {
		inventory.err = ErrObservation
		return inventory.err
	}
	want.verified = true
	inventory.entries[name] = want
	return nil
}

func (inventory *Inventory) Emit(ctx context.Context, stage Stage) error {
	if inventory == nil {
		return nil
	}
	if inventory.err != nil || stage == After != inventory.verifying {
		return ErrObservation
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	names := make([]string, 0, len(inventory.entries))
	for name, value := range inventory.entries {
		if len(names)%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if inventory.verifying && !value.verified {
			return ErrObservation
		}
		names = append(names, name)
	}
	slices.Sort(names)
	var stream Stream
	for index, name := range names {
		if index%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		value := inventory.entries[name]
		if err := stream.Add(Record{Path: name, Bytes: value.bytes, SHA256: "sha256:" + hex.EncodeToString(value.sum[:])}); err != nil {
			return err
		}
	}
	return Emit(ctx, Observation{Stage: stage, Path: inventory.component, Identity: stream.Identity()})
}

func safePath(value string) bool {
	return value != "" && value != "." && path.Clean(value) == value && !path.IsAbs(value) &&
		value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\\x00")
}
