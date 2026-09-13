package t421

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	maxExecutionAuthSocketPathBytes       = 103
	executionAuthorizationSchema          = "t422-execution-authorization-v1"
	executionAuthorizationSocketName      = "auth.sock"
	maxExecutionAuthorizationBytes        = 256
	maxExecutionAuthorizationMessageBytes = 4096
	maxExecutionAuthorizationReadBytes    = 4098
)

var errExecutionAuthorization = errors.New("execution authorization unavailable or changed")

// executionAuthorizationV1 is a trigger for one already-live private
// capability. It contains no reconstructible filesystem or process authority.
type executionAuthorizationV1 struct {
	Schema               string `json:"schema"`
	FreezeSHA256         string `json:"freeze_sha256"`
	SessionBindingSHA256 string `json:"session_binding_sha256"`
}

func canonicalExecutionAuthorization(value executionAuthorizationV1) ([]byte, error) {
	if value.Schema != executionAuthorizationSchema || !validExecutionHexSHA256(value.FreezeSHA256) ||
		!validExecutionHexSHA256(value.SessionBindingSHA256) {
		return nil, errExecutionAuthorization
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) == 0 || len(raw) > maxExecutionAuthorizationBytes {
		return nil, errExecutionAuthorization
	}
	return raw, nil
}

func decodeExecutionAuthorization(raw []byte) (executionAuthorizationV1, error) {
	if len(raw) == 0 || len(raw) > maxExecutionAuthorizationBytes {
		return executionAuthorizationV1{}, errExecutionAuthorization
	}
	var value executionAuthorizationV1
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return executionAuthorizationV1{}, errExecutionAuthorization
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return executionAuthorizationV1{}, errExecutionAuthorization
	}
	canonical, err := canonicalExecutionAuthorization(value)
	if err != nil || !bytes.Equal(canonical, raw) {
		return executionAuthorizationV1{}, errExecutionAuthorization
	}
	return value, nil
}

func validExecutionAuthorizationSocketPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && utf8.ValidString(path) &&
		!strings.ContainsRune(path, 0) && len([]byte(path)) <= maxExecutionAuthSocketPathBytes
}
