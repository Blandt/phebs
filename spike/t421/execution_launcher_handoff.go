//go:build darwin

package t421

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"time"
)

type executionAuthorizationHandoffFrame struct {
	value executionAuthorizationHandoffV1
	raw   []byte
	err   error
}

// Both message types share one buffered reader, preserving coalesced bytes.
func readExecutionAuthorizationHandoff(buffered *bufio.Reader) executionAuthorizationHandoffFrame {
	raw, err := buffered.ReadSlice('\n')
	if err != nil || len(raw) < 2 || len(raw) > maxExecutionAuthorizationHandoffFrameBytes {
		return executionAuthorizationHandoffFrame{err: errExecutionAuthorization}
	}
	raw = bytes.Clone(raw)
	value, err := decodeExecutionAuthorizationHandoff(raw)
	if err != nil {
		return executionAuthorizationHandoffFrame{err: errExecutionAuthorization}
	}
	return executionAuthorizationHandoffFrame{value: value, raw: raw}
}

type executionReturnedOutput struct {
	raw []byte
	err error
}

func captureExecutionReturnedOutput(reader io.Reader, frame chan<- executionAuthorizationHandoffFrame, returned chan<- executionReturnedOutput) {
	buffered := bufio.NewReaderSize(reader, maxExecutionAuthorizationHandoffFrameBytes)
	captured := readExecutionAuthorizationHandoff(buffered)
	frame <- captured
	if captured.err != nil {
		return
	}
	raw, err := captureExecutionReturnedPackage(buffered)
	returned <- executionReturnedOutput{raw: raw, err: err}
}

func forwardExecutionAuthorizationHandoff(ctx context.Context, output *executionAuthorizationOutput, raw []byte) (retErr error) {
	if ctx == nil || ctx.Err() != nil || output == nil || output.used || len(raw) < 2 || len(raw) > maxExecutionAuthorizationHandoffFrameBytes {
		return errExecutionAuthorization
	}
	output.used = true
	value, err := decodeExecutionAuthorizationHandoff(raw)
	if err != nil || output.check(ctx) != nil {
		return errExecutionAuthorization
	}
	deadline := time.Unix(0, value.FinalAdmissionDeadlineUnixNano)
	return writeExecutionOutput(ctx, output, raw, deadline)
}

// writeExecutionOutput preserves one cancellation/deadline corridor for both
// the early authorization handoff and the later authenticated package.
func writeExecutionOutput(ctx context.Context, output *executionAuthorizationOutput, raw []byte, deadline time.Time) (retErr error) {
	if ctx == nil || ctx.Err() != nil || output == nil || output.check(ctx) != nil {
		return errExecutionAuthorization
	}
	if output.deadline.Before(deadline) {
		deadline = output.deadline
	}
	if selected, ok := ctx.Deadline(); ok && selected.Before(deadline) {
		deadline = selected
	}
	if !time.Now().Before(deadline) || output.file.SetWriteDeadline(deadline) != nil || ctx.Err() != nil {
		return errExecutionAuthorization
	}
	joined := make(chan struct{})
	var cancellationErr error
	stop := context.AfterFunc(ctx, func() {
		cancellationErr = output.file.SetWriteDeadline(time.Now())
		close(joined)
	})
	defer func() {
		if !stop() {
			<-joined
		}
		if cancellationErr != nil || ctx.Err() != nil || !time.Now().Before(deadline) {
			retErr = errExecutionAuthorization
		}
	}()
	written, err := output.file.Write(raw)
	if err != nil || written != len(raw) || output.check(ctx) != nil {
		return errExecutionAuthorization
	}
	return nil
}
