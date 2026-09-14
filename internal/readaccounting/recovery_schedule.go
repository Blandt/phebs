package readaccounting

import (
	"context"
	"sync"
)

type RecoveryScheduleObservation struct {
	ScheduleSHA256 string `json:"schedule_sha256"`
	Chunks         uint64 `json:"chunks"`
	Successes      uint64 `json:"successes"`
}

type RecoveryScheduleScope struct {
	mu       sync.Mutex
	value    RecoveryScheduleObservation
	observed bool
}
type recoveryScheduleKey struct{}

// This request-local owner retains the latest already-validated native schedule
// read. It requests no extra query and adds no history or ordinary-mode state.
func CaptureRecoverySchedule(ctx context.Context) (context.Context, *RecoveryScheduleScope) {
	scope := &RecoveryScheduleScope{}
	return context.WithValue(ctx, recoveryScheduleKey{}, scope), scope
}

func ObserveRecoverySchedule(ctx context.Context, digest string, chunks, successes int) {
	if ctx == nil {
		return
	}
	scope, _ := ctx.Value(recoveryScheduleKey{}).(*RecoveryScheduleScope)
	if scope == nil {
		return
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	scope.observed = chunks > 0 && successes >= 0 && successes <= chunks && digest != ""
	if scope.observed {
		scope.value = RecoveryScheduleObservation{digest, uint64(chunks), uint64(successes)}
	}
}

func (scope *RecoveryScheduleScope) Observation() (RecoveryScheduleObservation, bool) {
	if scope == nil {
		return RecoveryScheduleObservation{}, false
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	return scope.value, scope.observed
}
