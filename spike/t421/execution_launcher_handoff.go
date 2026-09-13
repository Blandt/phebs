//go:build darwin

package t421

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"time"
)

type executionAuthorizationHandoffFrame struct {
	value executionAuthorizationHandoffV1
	raw   []byte
	err   error
}

// captureExecutionAuthorizationHandoff publishes the first complete canonical
// frame immediately, then separately proves that the child wrote no second
// byte before closing stdout. The fixed reader buffer prevents an unterminated
// or oversized line from allocating beyond the handoff ceiling.
func captureExecutionAuthorizationHandoff(
	reader io.Reader,
	frame chan<- executionAuthorizationHandoffFrame,
	tail chan<- error,
) {
	buffered := bufio.NewReaderSize(reader, maxExecutionAuthorizationHandoffFrameBytes)
	raw, err := buffered.ReadSlice('\n')
	if err != nil || len(raw) < 2 || len(raw) > maxExecutionAuthorizationHandoffFrameBytes {
		frame <- executionAuthorizationHandoffFrame{err: errExecutionAuthorization}
		return
	}
	raw = bytes.Clone(raw)
	value, err := decodeExecutionAuthorizationHandoff(raw)
	if err != nil {
		frame <- executionAuthorizationHandoffFrame{err: errExecutionAuthorization}
		return
	}
	frame <- executionAuthorizationHandoffFrame{value: value, raw: raw}
	if _, err := buffered.ReadByte(); !errors.Is(err, io.EOF) {
		tail <- errExecutionAuthorization
		return
	}
	tail <- nil
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
