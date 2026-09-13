package main

import (
	"context"
	"io"
	"log"
	"os"
	"sync"

	"github.com/bmeddeb/phebs/internal/dispatchadmission"
	"github.com/bmeddeb/phebs/internal/indexer"
)

const (
	t422ReuseCurrent     = byte('c')
	t422ReuseReactivated = byte('p')
)

const (
	t422ReuseSource = iota
	t422ReuseSearch
	t422ReuseObservation
	t422ReuseCatalog
	t422ReuseRelationship
	t422ReuseLaneCount
)

type t422ReusePhase struct {
	lanes             [t422ReuseLaneCount]byte
	pending, complete bool
}

type t422ReuseControl struct {
	mu      sync.Mutex
	initial dispatchadmission.ProductionSemanticSnapshot
	launch  *t422SemanticLaunch
	writer  io.Writer
	fail    func(error)
	failed  bool
	phases  [15]t422ReusePhase
}

func newT422ReuseControl(launch *t422SemanticLaunch, fail func(error)) (*t422ReuseControl, error) {
	if launch == nil {
		return nil, nil
	}
	initial, err := dispatchadmission.ProductionSemanticState()
	if err != nil || fail == nil || !launch.matches(initial) {
		return nil, errT422AttemptReport
	}
	writer, ok := log.Writer().(*os.File)
	if !ok || writer != os.Stderr {
		return nil, errT422AttemptReport
	}
	info, err := writer.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return nil, errT422AttemptReport
	}
	binding, err := t422SourceBinding(initial)
	if err != nil {
		return nil, err
	}
	copy(binding[:4], "RUB1")
	if n, err := writer.Write(binding); err != nil || n != len(binding) {
		fail(errT422AttemptReport)
		return nil, errT422AttemptReport
	}
	return &t422ReuseControl{initial: initial, launch: launch, writer: writer, fail: fail}, nil
}

func (control *t422ReuseControl) current(ctx context.Context) (dispatchadmission.ProductionSemanticSnapshot, error) {
	if control == nil || ctx == nil || ctx.Err() != nil {
		return dispatchadmission.ProductionSemanticSnapshot{}, errT422AttemptReport
	}
	current, err := dispatchadmission.ProductionSemanticState()
	if err != nil || !control.launch.matches(current) {
		return dispatchadmission.ProductionSemanticSnapshot{}, errT422AttemptReport
	}
	if _, err := t422SourceRecord(current, control.initial); err != nil {
		return dispatchadmission.ProductionSemanticSnapshot{}, err
	}
	return current, nil
}

func (control *t422ReuseControl) refuse() error {
	if control == nil {
		return errT422AttemptReport
	}
	control.mu.Lock()
	first := !control.failed
	control.failed = true
	control.mu.Unlock()
	if first {
		control.fail(errT422AttemptReport)
	}
	return errT422AttemptReport
}

func (control *t422ReuseControl) observe(ctx context.Context, repository string, first, last int, decision byte) error {
	current, err := control.current(ctx)
	if err != nil {
		return control.refuse()
	}
	return control.observeCurrent(current, repository, first, last, decision)
}

func (control *t422ReuseControl) observeCurrent(current dispatchadmission.ProductionSemanticSnapshot, repository string, first, last int, decision byte) error {
	if control == nil || control.launch == nil || repository != control.launch.request.Repository ||
		first < 0 || last < first || last >= t422ReuseLaneCount || current.Phase < 1 || current.Phase > 15 ||
		decision != t422ReuseCurrent && decision != t422ReuseReactivated {
		return control.refuse()
	}
	phase := &control.phases[current.Phase-1]
	control.mu.Lock()
	valid := !control.failed && !phase.pending && !phase.complete
	for lane := first; valid && lane <= last; lane++ {
		valid = phase.lanes[lane] == 0 || phase.lanes[lane] == decision
	}
	if valid {
		for lane := first; lane <= last; lane++ {
			phase.lanes[lane] = decision
		}
	}
	control.mu.Unlock()
	if !valid {
		return control.refuse()
	}
	return nil
}

func (control *t422ReuseControl) observeIndex(ctx context.Context, repository, commit string, decision indexer.IndexReuseDecision) error {
	if control.launch.request.ReturnSourceCommit != "" && commit != control.launch.request.ReturnSourceCommit {
		return control.refuse()
	}
	code := t422ReuseCurrent
	if decision == indexer.IndexReuseReactivated {
		code = t422ReuseReactivated
	} else if decision != indexer.IndexReuseCurrent {
		return control.refuse()
	}
	return control.observe(ctx, repository, t422ReuseSource, t422ReuseSearch, code)
}

func (control *t422ReuseControl) observeObservation(ctx context.Context, repository string) error {
	return control.observe(ctx, repository, t422ReuseObservation, t422ReuseObservation, t422ReuseCurrent)
}

func (control *t422ReuseControl) observeCatalog(ctx context.Context, repository string) error {
	return control.observe(ctx, repository, t422ReuseCatalog, t422ReuseCatalog, t422ReuseCurrent)
}

func (control *t422ReuseControl) observeRelationship(ctx context.Context, repository string) error {
	return control.observe(ctx, repository, t422ReuseRelationship, t422ReuseRelationship, t422ReuseCurrent)
}

// finalTail turns absent lanes into known zero only after the exact F body,
// cache commit, accounting ledger and report sink have all completed.
func (control *t422ReuseControl) finalTail(ctx context.Context, queryTerminal bool, prior func(error)) (func(error), error) {
	current, err := control.current(ctx)
	if err != nil || !control.launch.requestCurrent(ctx) {
		return nil, control.refuse()
	}
	started, err := control.beginFinal(current, queryTerminal)
	if err != nil {
		return nil, control.refuse()
	}
	if !started {
		return prior, nil
	}
	phase := current.Phase
	return func(cause error) {
		if prior != nil {
			prior(cause)
		}
		if cause != nil {
			control.abortFinal(phase)
			return
		}
		latest, stateErr := control.current(ctx)
		if stateErr != nil || latest.Phase != phase || !control.launch.requestCurrent(ctx) || control.finishFinal(latest) != nil {
			_ = control.refuse()
		}
	}, nil
}

func (control *t422ReuseControl) beginFinal(current dispatchadmission.ProductionSemanticSnapshot, queryTerminal bool) (bool, error) {
	if control == nil || current.Phase < 1 || current.Phase > 15 {
		return false, errT422AttemptReport
	}
	phase := &control.phases[current.Phase-1]
	control.mu.Lock()
	if control.failed || phase.pending {
		control.mu.Unlock()
		return false, errT422AttemptReport
	}
	if current.Phase == 14 && !queryTerminal {
		control.mu.Unlock()
		return false, nil
	}
	if queryTerminal != (current.Phase == 14) {
		control.mu.Unlock()
		return false, errT422AttemptReport
	}
	if phase.complete {
		control.mu.Unlock()
		return false, nil
	}
	phase.pending = true
	control.mu.Unlock()
	return true, nil
}

func (control *t422ReuseControl) abortFinal(phase uint32) {
	if control == nil || phase < 1 || phase > 15 {
		return
	}
	control.mu.Lock()
	control.phases[phase-1].pending = false
	control.mu.Unlock()
}

func (control *t422ReuseControl) finishFinal(current dispatchadmission.ProductionSemanticSnapshot) error {
	if control == nil || current.Phase < 1 || current.Phase > 15 {
		return errT422AttemptReport
	}
	phase := &control.phases[current.Phase-1]
	control.mu.Lock()
	valid := !control.failed && phase.pending && !phase.complete
	lanes := phase.lanes
	control.mu.Unlock()
	if !valid {
		return errT422AttemptReport
	}
	raw, err := t422ReuseRecord(current, control.initial, lanes)
	if err == nil {
		var n int
		n, err = control.writer.Write(raw[:])
		if err == nil && n != len(raw) {
			err = io.ErrShortWrite
		}
	}
	if err != nil {
		return errT422AttemptReport
	}
	control.mu.Lock()
	phase.pending, phase.complete = false, true
	control.mu.Unlock()
	return nil
}

func t422ReuseRecord(current, initial dispatchadmission.ProductionSemanticSnapshot, lanes [t422ReuseLaneCount]byte) ([14]byte, error) {
	var record [14]byte
	if _, err := t422SourceRecord(current, initial); err != nil || lanes[t422ReuseSource] != lanes[t422ReuseSearch] {
		return record, errT422AttemptReport
	}
	for lane, decision := range lanes {
		switch decision {
		case 0:
			lanes[lane] = '0'
		case t422ReuseCurrent:
		case t422ReuseReactivated:
			if lane > t422ReuseSearch {
				return record, errT422AttemptReport
			}
		default:
			return record, errT422AttemptReport
		}
	}
	copy(record[:], []byte{'R', 'U', '1', ':', "0123456789ABCDEF"[current.ProducerID], ':', "0123456789ABCDEF"[current.Phase], ':'})
	copy(record[8:13], lanes[:])
	record[13] = '\n'
	return record, nil
}
