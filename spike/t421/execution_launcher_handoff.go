//go:build darwin

package t421

import (
	"bufio"
	"bytes"
	"errors"
	"io"
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

func forwardExecutionAuthorizationHandoff(writer io.Writer, raw []byte) error {
	if writer == nil || len(raw) < 2 || len(raw) > maxExecutionAuthorizationHandoffFrameBytes {
		return errExecutionAuthorization
	}
	if _, err := decodeExecutionAuthorizationHandoff(raw); err != nil {
		return errExecutionAuthorization
	}
	written, err := writer.Write(raw)
	if err != nil || written != len(raw) {
		return errExecutionAuthorization
	}
	return nil
}
