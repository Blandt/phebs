package t421

import (
	"bytes"
	"io"
	"math"
	"time"
)

func executionFinalAdmissionDeadline(firstVerifiedAt, outerDeadline time.Time, revalidationDeadlineMS uint64) (time.Time, error) {
	first, outer := firstVerifiedAt.UnixNano(), outerDeadline.UnixNano()
	if first <= 0 || outer <= 0 || first >= outer || revalidationDeadlineMS == 0 || revalidationDeadlineMS > math.MaxInt64/uint64(time.Millisecond) {
		return time.Time{}, errExecutionAuthorization
	}
	window := int64(revalidationDeadlineMS) * int64(time.Millisecond)
	if first > math.MaxInt64-window {
		return time.Time{}, errExecutionAuthorization
	}
	final := first + window
	if final > outer {
		final = outer
	}
	return time.Unix(0, final), nil
}

func emitExecutionAuthorizationHandoff(writer io.Writer, handoff executionAuthorizationHandoff) error {
	if writer == nil || len(handoff.frame) < 2 || len(handoff.frame) > maxExecutionAuthorizationHandoffFrameBytes ||
		!validExecutionHexSHA256(handoff.frameSHA256) || handoff.frameSHA256 != executionAuthorizationSHA256(handoff.frame) ||
		!bytes.Equal(handoff.authorization, []byte(handoff.value.AuthorizationJSON)) {
		return errExecutionAuthorization
	}
	decoded, err := decodeExecutionAuthorizationHandoff(handoff.frame)
	if err != nil || !equalExecutionAuthorizationHandoff(decoded, handoff.value) {
		return errExecutionAuthorization
	}
	written, err := writer.Write(handoff.frame)
	if err != nil || written != len(handoff.frame) {
		return errExecutionAuthorization
	}
	return nil
}
