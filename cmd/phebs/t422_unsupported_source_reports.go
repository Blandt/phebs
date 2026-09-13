package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"sync"

	"github.com/bmeddeb/phebs/internal/dispatchadmission"
	"github.com/bmeddeb/phebs/internal/readaccounting"
)

func t422UnsupportedSourceBinding(state dispatchadmission.ProductionSemanticSnapshot) ([]byte, error) {
	raw, err := t422SourceBinding(state)
	if err != nil {
		return nil, err
	}
	raw[0], raw[1] = 'U', 'F'
	return raw, nil
}

func t422UnsupportedSourceRecord(current, initial dispatchadmission.ProductionSemanticSnapshot, event readaccounting.UnsupportedSourceObservation) ([]byte, error) {
	if _, err := t422SourceRecord(current, initial); err != nil {
		return nil, errT422AttemptReport
	}
	if event.Unsupported > math.MaxUint32 {
		return nil, errT422AttemptReport
	}
	return []byte(fmt.Sprintf("UF1:%X:%X:%08x\n", current.ProducerID, current.Phase, event.Unsupported)), nil
}

type t422UnsupportedSourceControl struct {
	mu      sync.Mutex
	initial dispatchadmission.ProductionSemanticSnapshot
	writer  *os.File
	fail    func(error)
}

func (control *t422UnsupportedSourceControl) observe(event readaccounting.UnsupportedSourceObservation) (uint32, error) {
	control.mu.Lock()
	defer control.mu.Unlock()
	current, err := dispatchadmission.ProductionWorkState()
	if err != nil {
		control.fail(errT422AttemptReport)
		return 0, errT422AttemptReport
	}
	return control.write(current, event)
}

func (control *t422UnsupportedSourceControl) write(current dispatchadmission.ProductionSemanticSnapshot, event readaccounting.UnsupportedSourceObservation) (uint32, error) {
	raw, err := t422UnsupportedSourceRecord(current, control.initial, event)
	if err == nil {
		var n int
		n, err = control.writer.Write(raw)
		if err == nil && n != len(raw) {
			err = io.ErrShortWrite
		}
	}
	if err != nil {
		control.fail(errT422AttemptReport)
		return 0, errT422AttemptReport
	}
	if event.Unsupported != 0 {
		control.fail(errT422AttemptReport)
		return 0, errT422AttemptReport
	}
	return current.Phase, nil
}

func bindT422UnsupportedSourceReports(ctx context.Context, initial dispatchadmission.ProductionSemanticSnapshot, fail func(error)) (context.Context, error) {
	raw, err := t422UnsupportedSourceBinding(initial)
	writer, ok := log.Writer().(*os.File)
	if ctx == nil || err != nil || fail == nil || !ok || writer != os.Stderr {
		return nil, errT422AttemptReport
	}
	info, err := writer.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return nil, errT422AttemptReport
	}
	if n, err := writer.Write(raw); err != nil || n != len(raw) {
		fail(errT422AttemptReport)
		return nil, errT422AttemptReport
	}
	control := &t422UnsupportedSourceControl{initial: initial, writer: writer, fail: fail}
	return readaccounting.WithUnsupportedSourceObserver(ctx, control.observe)
}
