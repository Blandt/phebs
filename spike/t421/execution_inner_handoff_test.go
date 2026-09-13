package t421

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestExecutionFinalAdmissionDeadlineAnchorsAtFirstVerification(t *testing.T) {
	first := time.Unix(100, 123)
	outer := first.Add(30 * time.Minute)
	deadline, err := executionFinalAdmissionDeadline(first, outer, 20*60*1000)
	if err != nil || !deadline.Equal(first.Add(20*time.Minute)) {
		t.Fatalf("unclipped deadline = %v, %v", deadline, err)
	}
	deadline, err = executionFinalAdmissionDeadline(first, first.Add(5*time.Minute), 20*60*1000)
	if err != nil || !deadline.Equal(first.Add(5*time.Minute)) {
		t.Fatalf("clipped deadline = %v, %v", deadline, err)
	}
	for _, test := range []struct {
		first, outer time.Time
		window       uint64
	}{
		{time.Time{}, outer, 1},
		{first, first, 1},
		{first, outer, 0},
		{time.Unix(0, math.MaxInt64-10), time.Unix(0, math.MaxInt64-1), 1},
		{first, outer, uint64(math.MaxInt64)/uint64(time.Millisecond) + 1},
	} {
		if _, err := executionFinalAdmissionDeadline(test.first, test.outer, test.window); err == nil {
			t.Fatalf("invalid deadline admitted: %+v", test)
		}
	}
}

func TestExecutionAuthorizationHandoffEmitsExactlyOneStrictFrame(t *testing.T) {
	executePath, socketPath := "/tmp/t422-execute", "/tmp/t422/auth.sock"
	image := "sha256:" + strings.Repeat("c", 64)
	projection, err := projectExecutionAuthorizationHandoff(executePath, socketPath, image, 20)
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := buildExecutionAuthorizationHandoff(executePath, socketPath, image, 20, 19,
		strings.Repeat("a", 64), strings.Repeat("b", 64), projection)
	if err != nil {
		t.Fatal(err)
	}
	w := &executionHandoffTestWriter{}
	if err := emitExecutionAuthorizationHandoff(w, handoff); err != nil || w.calls != 1 || !bytes.Equal(w.raw, handoff.frame) {
		t.Fatalf("exact frame emission = calls %d bytes %d err %v", w.calls, len(w.raw), err)
	}
	for _, mutate := range []func(*executionAuthorizationHandoff){
		func(value *executionAuthorizationHandoff) { value.frameSHA256 = strings.Repeat("0", 64) },
		func(value *executionAuthorizationHandoff) { value.authorization = append(value.authorization, ' ') },
		func(value *executionAuthorizationHandoff) { value.value.SocketPath += "-changed" },
	} {
		changed := handoff
		changed.authorization = bytes.Clone(handoff.authorization)
		changed.frame = bytes.Clone(handoff.frame)
		mutate(&changed)
		refused := &executionHandoffTestWriter{}
		if err := emitExecutionAuthorizationHandoff(refused, changed); err == nil || refused.calls != 0 {
			t.Fatal("invalid handoff reached output")
		}
	}
	for _, refused := range []*executionHandoffTestWriter{{short: true}, {err: errors.New("closed")}} {
		if err := emitExecutionAuthorizationHandoff(refused, handoff); err == nil || refused.calls != 1 {
			t.Fatal("failed output did not refuse after one write")
		}
	}
}

type executionHandoffTestWriter struct {
	calls int
	raw   []byte
	short bool
	err   error
}

func (writer *executionHandoffTestWriter) Write(raw []byte) (int, error) {
	writer.calls++
	writer.raw = append(writer.raw, raw...)
	if writer.short {
		return len(raw) - 1, nil
	}
	if writer.err != nil {
		return 0, writer.err
	}
	return len(raw), nil
}
