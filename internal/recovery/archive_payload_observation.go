package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/bmeddeb/phebs/internal/archiveevidence"
)

// Database inventory records are the exact native DEFINE or at-most-512-record
// INSERT payloads, in source order. OPTION/INSERT brackets, ignored trivia and
// transport BEGIN/COMMIT wrappers are excluded consistently at all three reads.
// The after stream measures consumed HTTP bodies accepted by every statement
// and COMMIT result. It is not a post-repair live database census.
func databasePayloadRecord(ordinal uint64, unit restoreReplayUnit, sum [32]byte) archiveevidence.Record {
	kind := "insert"
	if unit.Definition {
		kind = "define"
	}
	return archiveevidence.Record{
		Path:   fmt.Sprintf("units/%020d/%s", ordinal, kind),
		Bytes:  uint64(unit.Span.End - unit.Span.Start),
		SHA256: "sha256:" + hex.EncodeToString(sum[:]),
	}
}

func (scanner *restoreReplayScanner) startPayload() {
	if scanner.measurePayloads {
		if scanner.payloadBuffer == nil {
			scanner.payloadBuffer = make([]byte, restoreReplayBufferBytes)
		}
		if scanner.payloadHash == nil {
			scanner.payloadHash = sha256.New()
		} else {
			scanner.payloadHash.Reset()
		}
		scanner.payload = scanner.payloadHash
		scanner.payloadBuffered = 0
	}
}

func (scanner *restoreReplayScanner) flushPayload() {
	_, _ = scanner.payload.Write(scanner.payloadBuffer[:scanner.payloadBuffered])
	scanner.payloadBuffered = 0
}

func (scanner *restoreReplayScanner) payloadDigest(unit *restoreReplayUnit) {
	if scanner.payload != nil {
		scanner.flushPayload()
		_ = scanner.payload.Sum(unit.PayloadSHA256[:0])
	}
}

func scanDatabasePayloads(reader io.Reader) (archiveevidence.Identity, error) {
	scanner := newRestoreReplayScanner(reader)
	scanner.measurePayloads = true
	var stream archiveevidence.Stream
	for ordinal := uint64(1); ; ordinal++ {
		unit, err := scanner.next()
		if err == io.EOF {
			return stream.Identity(), nil
		}
		if err != nil {
			return archiveevidence.Identity{}, err
		}
		if err := stream.Add(databasePayloadRecord(ordinal, unit, unit.PayloadSHA256)); err != nil {
			return archiveevidence.Identity{}, err
		}
	}
}

type databaseSourceObservationKey struct{}

func databaseSourceObservationContext(ctx context.Context) context.Context {
	if !archiveevidence.Selected(ctx) {
		return ctx
	}
	return context.WithValue(ctx, databaseSourceObservationKey{}, true)
}

type payloadReadCounter struct {
	reader io.Reader
	bytes  int64
}

func (counter *payloadReadCounter) Read(raw []byte) (int, error) {
	n, err := counter.reader.Read(raw)
	counter.bytes += int64(n)
	return n, err
}
