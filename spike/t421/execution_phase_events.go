package t421

import (
	"errors"
	"slices"
	"sync"
	"time"
)

type executionPhaseEventSlot struct {
	value   PhaseMeasurement
	started time.Time
}

// executionPhaseEventRecorder is created only after final admission consumes
// reserved ordinal one. Its fixed inventory always snapshots in frozen order;
// an unattempted suffix is explicit and carries no event or wall evidence.
type executionPhaseEventRecorder struct {
	mu              sync.Mutex
	ordinals        *admittedExecutionEventOrdinals
	outerStarted    time.Time
	outerDeadline   time.Time
	lastFinished    time.Time
	phases          []string
	slots           []executionPhaseEventSlot
	next, active    int
	stopped, failed bool
}

func newExecutionPhaseEventRecorder(
	ordinals *admittedExecutionEventOrdinals,
	phases []string,
	started, deadline time.Time,
) (*executionPhaseEventRecorder, error) {
	want := frozenPhaseOrder()
	if ordinals == nil || ordinals.owner == nil || started.IsZero() || !started.Before(deadline) ||
		!slices.Equal(phases, want) {
		return nil, ErrExecutionEpochOne
	}
	return &executionPhaseEventRecorder{
		ordinals: ordinals, outerStarted: started, outerDeadline: deadline,
		phases: slices.Clone(phases), slots: make([]executionPhaseEventSlot, len(phases)), active: -1,
	}, nil
}

func (recorder *executionPhaseEventRecorder) begin(phase string) error {
	return recorder.beginAt(phase, time.Now())
}

func (recorder *executionPhaseEventRecorder) beginAt(phase string, started time.Time) error {
	if recorder == nil {
		return ErrExecutionEpochOne
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	last := len(recorder.phases) - 1
	index := recorder.next
	if recorder.stopped && index < last && phase == recorder.phases[last] {
		index = last
	}
	if recorder.failed || recorder.active != -1 || index < 0 || index >= len(recorder.phases) ||
		recorder.phases[index] != phase || started.Before(recorder.outerStarted) ||
		(!recorder.lastFinished.IsZero() && started.Before(recorder.lastFinished)) || index != last && !started.Before(recorder.outerDeadline) {
		recorder.failed = true
		return ErrExecutionEpochOne
	}
	ordinal, err := recorder.ordinals.next()
	if err != nil || ordinal <= 1 {
		recorder.failed = true
		return ErrExecutionEpochOne
	}
	recorder.active = index
	recorder.slots[index] = executionPhaseEventSlot{
		value:   PhaseMeasurement{Phase: phase, StartEventOrdinal: ordinal},
		started: started,
	}
	return nil
}

func (recorder *executionPhaseEventRecorder) finish(phase, outcome string) error {
	return recorder.finishAt(phase, outcome, time.Now())
}

func (recorder *executionPhaseEventRecorder) event(phase string) (uint64, error) {
	if recorder == nil {
		return 0, ErrExecutionEpochOne
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.failed || recorder.active < 0 || recorder.active >= len(recorder.phases) ||
		recorder.phases[recorder.active] != phase {
		recorder.failed = true
		return 0, ErrExecutionEpochOne
	}
	ordinal, err := recorder.ordinals.next()
	if err != nil || ordinal <= recorder.slots[recorder.active].value.StartEventOrdinal {
		recorder.failed = true
		return 0, ErrExecutionEpochOne
	}
	return ordinal, nil
}

func (recorder *executionPhaseEventRecorder) finishAt(phase, outcome string, finished time.Time) error {
	if recorder == nil {
		return ErrExecutionEpochOne
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	index := recorder.active
	last := len(recorder.phases) - 1
	validOutcome := index == last && (outcome == "clean" || outcome == "failed") ||
		index >= 0 && index < last && (outcome == "passed" || outcome == "stopped")
	if recorder.failed || index < 0 || index >= len(recorder.slots) || recorder.phases[index] != phase ||
		recorder.slots[index].value.Phase != phase || !validOutcome || finished.Before(recorder.slots[index].started) {
		recorder.failed = true
		return ErrExecutionEpochOne
	}
	ordinal, err := recorder.ordinals.next()
	if err != nil || ordinal <= recorder.slots[index].value.StartEventOrdinal {
		recorder.failed = true
		return ErrExecutionEpochOne
	}
	elapsed := finished.Sub(recorder.slots[index].started)
	wall := uint64(elapsed / time.Millisecond)
	if elapsed%time.Millisecond != 0 {
		wall++
	}
	if wall == 0 {
		wall = 1
	}
	recorder.slots[index].value.FinishEventOrdinal = ordinal
	recorder.slots[index].value.Metrics.WallMS = Milliseconds(wall)
	recorder.lastFinished = finished
	recorder.active = -1
	recorder.next = index + 1
	if outcome == "stopped" {
		recorder.stopped = true
	}
	return nil
}

func (recorder *executionPhaseEventRecorder) snapshot() ([]PhaseMeasurement, error) {
	if recorder == nil {
		return nil, ErrExecutionEpochOne
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.failed || recorder.active != -1 {
		return nil, ErrExecutionEpochOne
	}
	result := make([]PhaseMeasurement, len(recorder.phases))
	for index, phase := range recorder.phases {
		value := recorder.slots[index].value
		if value.Phase == "" {
			value.Phase = phase
		}
		result[index] = value
	}
	return result, nil
}

func cloneExecutionPhaseEvents(values []PhaseMeasurement) []PhaseMeasurement {
	return slices.Clone(values)
}

func (flow *ExecutionEpochOne) beginExecutionPhase(phase string) error {
	if flow == nil {
		return ErrExecutionEpochOne
	}
	flow.mu.Lock()
	recorder := flow.executionPhaseEvents
	flow.mu.Unlock()
	return recorder.begin(phase)
}

func (flow *ExecutionEpochOne) hasExecutionPhaseEvents() bool {
	if flow == nil {
		return false
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	return flow.executionPhaseEvents != nil
}

func (flow *ExecutionEpochOne) finishExecutionPhase(phase, outcome string) error {
	if flow == nil {
		return ErrExecutionEpochOne
	}
	flow.mu.Lock()
	recorder := flow.executionPhaseEvents
	flow.mu.Unlock()
	return recorder.finish(phase, outcome)
}

func (flow *ExecutionEpochOne) recordExecutionEvent(phase string) (uint64, error) {
	if flow == nil {
		return 0, ErrExecutionEpochOne
	}
	flow.mu.Lock()
	recorder := flow.executionPhaseEvents
	flow.mu.Unlock()
	return recorder.event(phase)
}

// Named events mark when the controller accepts an observation. Composite
// observations may describe earlier native operations; these times are not
// native callback timestamps or measurements of the operations' duration.
func (flow *ExecutionEpochOne) recordNamedExecutionEvent(phase, name string) (uint64, error) {
	if flow == nil || name == "" {
		return 0, ErrExecutionEpochOne
	}
	ordinal, err := flow.recordExecutionEvent(phase)
	if err != nil {
		return 0, err
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.executionEvidenceEvents == nil || flow.executionEvidenceEvents[name] != 0 {
		return 0, ErrExecutionEpochOne
	}
	flow.executionEvidenceEvents[name] = ordinal
	flow.executionEvidenceTimes[name] = time.Now()
	return ordinal, nil
}

func (flow *ExecutionEpochOne) recordOptionalNamedExecutionEvent(phase, name string) (uint64, error) {
	if !flow.hasExecutionPhaseEvents() {
		return 0, nil
	}
	return flow.recordNamedExecutionEvent(phase, name)
}

func (flow *ExecutionEpochOne) executionNamedEventEvidence() map[string]uint64 {
	if flow == nil {
		return nil
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	result := make(map[string]uint64, len(flow.executionEvidenceEvents))
	for name, ordinal := range flow.executionEvidenceEvents {
		result[name] = ordinal
	}
	return result
}

func (flow *ExecutionEpochOne) executionNamedEventTimes() map[string]time.Time {
	if flow == nil {
		return nil
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	result := make(map[string]time.Time, len(flow.executionEvidenceTimes))
	for name, observed := range flow.executionEvidenceTimes {
		result[name] = observed
	}
	return result
}

func (flow *ExecutionEpochOne) executionPhaseEventEvidence() ([]PhaseMeasurement, error) {
	if flow == nil {
		return nil, ErrExecutionEpochOne
	}
	flow.mu.Lock()
	recorder := flow.executionPhaseEvents
	flow.mu.Unlock()
	return recorder.snapshot()
}

func runExecutionPhase(flow *ExecutionEpochOne, phase string, operation func() error) error {
	if operation == nil || flow.beginExecutionPhase(phase) != nil {
		return ErrExecutionEpochOne
	}
	err := operation()
	outcome := "passed"
	if err != nil {
		outcome = "stopped"
	}
	if phase == "teardown" {
		outcome = "clean"
		if err != nil {
			outcome = "failed"
		}
	}
	if finishErr := flow.finishExecutionPhase(phase, outcome); finishErr != nil {
		err = errors.Join(err, finishErr)
	}
	if err != nil {
		return errors.Join(ErrExecutionEpochOne, err)
	}
	return nil
}
