package t421

import (
	"errors"
	"math"
	"sync"
)

var errExecutionEventOrdinal = errors.New("T42.2 global event ordinal unavailable")

// executionEventOrdinals owns the one process-wide ordering stream used by a
// prospective T42.2 execution. It must not be copied. There is deliberately no
// decoder, reset, caller-selected starting point, or exported constructor.
//
// The future final-admission issuer owns consumeFinalAdmission. Operational
// composition receives only the opaque admitted handle, so it cannot allocate
// ordinal one or begin a second stream.
type executionEventOrdinals struct {
	mu     sync.Mutex
	last   uint64
	failed bool
}

type admittedExecutionEventOrdinals struct {
	owner *executionEventOrdinals
}

func newExecutionEventOrdinals() *executionEventOrdinals {
	return &executionEventOrdinals{}
}

// consumeFinalAdmission consumes the globally reserved ordinal one and yields
// the only handle that can allocate later event ordinals. Until the real
// signature-verification issuer exists this API remains unintegrated.
func (ordinals *executionEventOrdinals) consumeFinalAdmission() (*admittedExecutionEventOrdinals, uint64, error) {
	if ordinals == nil {
		return nil, 0, errExecutionEventOrdinal
	}
	ordinals.mu.Lock()
	defer ordinals.mu.Unlock()
	if ordinals.failed || ordinals.last != 0 {
		ordinals.failed = true
		return nil, 0, errExecutionEventOrdinal
	}
	ordinals.last = 1
	return &admittedExecutionEventOrdinals{owner: ordinals}, 1, nil
}

func (admitted *admittedExecutionEventOrdinals) next() (uint64, error) {
	if admitted == nil || admitted.owner == nil {
		return 0, errExecutionEventOrdinal
	}
	ordinals := admitted.owner
	ordinals.mu.Lock()
	defer ordinals.mu.Unlock()
	if ordinals.failed || ordinals.last < 1 || ordinals.last == math.MaxUint64 {
		ordinals.failed = true
		return 0, errExecutionEventOrdinal
	}
	ordinals.last++
	return ordinals.last, nil
}
