package t421

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	executionAuthorizationSessionBindingSchema = "t422-execution-authorization-session-binding-v1"
	executionAuthorizationHandoffSchema        = "t422-execution-authorization-handoff-v1"
	maxExecutionAuthorizationSessionBytes      = 4 << 10
	maxExecutionAuthorizationHandoffFrameBytes = 16 << 10
)

type executionAuthorizationSessionBindingPreimageV1 struct {
	Schema                         string `json:"schema"`
	CeremonyID                     string `json:"ceremony_id"`
	FreezeSHA256                   string `json:"freeze_sha256"`
	OuterPID                       int64  `json:"outer_pid"`
	OuterStartToken                string `json:"outer_start_token"`
	InnerPID                       int64  `json:"inner_pid"`
	InnerParentPID                 int64  `json:"inner_parent_pid"`
	InnerStartToken                string `json:"inner_start_token"`
	InnerSessionID                 int64  `json:"inner_session_id"`
	InnerProcessGroupID            int64  `json:"inner_process_group_id"`
	T422ExecuteCanonicalPathSHA256 string `json:"t422_execute_canonical_path_sha256"`
	T422ExecuteDevice              int64  `json:"t422_execute_st_dev"`
	T422ExecuteInode               uint64 `json:"t422_execute_st_ino"`
	T422ExecuteMode                uint32 `json:"t422_execute_st_mode"`
	T422ExecuteSize                int64  `json:"t422_execute_size"`
	T422ExecuteCTimeUnixNano       int64  `json:"t422_execute_ctime_unix_nano"`
	T422ExecuteImageSHA256         string `json:"t422_execute_image_sha256"`
	ListenerParentDevice           int64  `json:"listener_parent_st_dev"`
	ListenerParentInode            uint64 `json:"listener_parent_st_ino"`
	ListenerDevice                 int64  `json:"listener_st_dev"`
	ListenerInode                  uint64 `json:"listener_st_ino"`
	ListenerMode                   uint32 `json:"listener_st_mode"`
	SocketPathSHA256               string `json:"socket_path_sha256"`
	OuterDeadlineUnixNano          int64  `json:"outer_deadline_unix_nano"`
	FinalAdmissionDeadlineUnixNano int64  `json:"final_admission_deadline_unix_nano"`
}

type executionAuthorizationSessionBinding struct {
	preimage executionAuthorizationSessionBindingPreimageV1
	raw      []byte
	sha256   string
}

type executionAuthorizationHandoffV1 struct {
	Schema                         string   `json:"schema"`
	SocketPath                     string   `json:"socket_path"`
	AuthorizationJSON              string   `json:"authorization_json"`
	AuthorizationSHA256            string   `json:"authorization_sha256"`
	PayloadBase64URL               string   `json:"payload_base64url"`
	SessionBindingSHA256           string   `json:"session_binding_sha256"`
	FreezeSHA256                   string   `json:"freeze_sha256"`
	T422ExecuteImageSHA256         string   `json:"t422_execute_image_sha256"`
	OuterDeadlineUnixNano          int64    `json:"outer_deadline_unix_nano"`
	FinalAdmissionDeadlineUnixNano int64    `json:"final_admission_deadline_unix_nano"`
	ClientArgv                     []string `json:"client_argv"`
	ClientArgvSHA256               string   `json:"client_argv_sha256"`
	RenderedClientCommand          string   `json:"rendered_client_command"`
	RenderedClientCommandSHA256    string   `json:"rendered_client_command_sha256"`
}

type executionAuthorizationHandoffProjection struct {
	frameBytes  uint64
	frameSHA256 string
}

type executionAuthorizationHandoff struct {
	value         executionAuthorizationHandoffV1
	authorization []byte
	frame         []byte
	frameSHA256   string
}

func buildExecutionAuthorizationSessionBinding(value executionAuthorizationSessionBindingPreimageV1) (executionAuthorizationSessionBinding, error) {
	if !validExecutionAuthorizationSessionBinding(value) {
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) == 0 || len(raw) > maxExecutionAuthorizationSessionBytes {
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}
	decoded, err := decodeExecutionAuthorizationSessionBinding(raw)
	if err != nil || decoded != value {
		return executionAuthorizationSessionBinding{}, errExecutionAuthorization
	}
	return executionAuthorizationSessionBinding{preimage: value, raw: raw, sha256: executionAuthorizationSHA256(raw)}, nil
}

func decodeExecutionAuthorizationSessionBinding(raw []byte) (executionAuthorizationSessionBindingPreimageV1, error) {
	var value executionAuthorizationSessionBindingPreimageV1
	if len(raw) == 0 || len(raw) > maxExecutionAuthorizationSessionBytes || decodeExecutionAuthorizationCanonical(raw, &value) != nil ||
		!validExecutionAuthorizationSessionBinding(value) {
		return value, errExecutionAuthorization
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(raw, canonical) {
		return executionAuthorizationSessionBindingPreimageV1{}, errExecutionAuthorization
	}
	return value, nil
}

func validExecutionAuthorizationSessionBinding(value executionAuthorizationSessionBindingPreimageV1) bool {
	validStart := func(token string) bool {
		return token != "" && len(token) <= 64 && utf8.ValidString(token) && !strings.ContainsRune(token, 0)
	}
	return value.Schema == executionAuthorizationSessionBindingSchema &&
		executionCeremonyID.MatchString(value.CeremonyID) && value.CeremonyID != "." && value.CeremonyID != ".." &&
		validExecutionHexSHA256(value.FreezeSHA256) &&
		value.OuterPID > 0 && value.InnerPID > 0 && value.InnerPID != value.OuterPID && value.InnerParentPID == value.OuterPID &&
		value.InnerSessionID == value.InnerPID && value.InnerProcessGroupID == value.InnerPID &&
		validStart(value.OuterStartToken) && validStart(value.InnerStartToken) &&
		validExecutionHexSHA256(value.T422ExecuteCanonicalPathSHA256) &&
		value.T422ExecuteDevice >= 0 && value.T422ExecuteInode > 0 && value.T422ExecuteMode == 0o100500 &&
		value.T422ExecuteSize > 0 && value.T422ExecuteCTimeUnixNano > 0 && validExecutionSHA256(value.T422ExecuteImageSHA256) &&
		value.ListenerParentDevice >= 0 && value.ListenerParentInode > 0 &&
		value.ListenerDevice >= 0 && value.ListenerInode > 0 && value.ListenerMode == 0o140600 &&
		validExecutionHexSHA256(value.SocketPathSHA256) &&
		value.OuterDeadlineUnixNano > 0 && value.FinalAdmissionDeadlineUnixNano > 0 &&
		value.FinalAdmissionDeadlineUnixNano <= value.OuterDeadlineUnixNano
}

func projectExecutionAuthorizationHandoff(executePath, socketPath, executeImageSHA256 string, outerDeadlineUnixNano, finalDeadlineUnixNano int64) (executionAuthorizationHandoffProjection, error) {
	placeholder := strings.Repeat("0", 64)
	_, _, frame, err := assembleExecutionAuthorizationHandoff(executePath, socketPath, executeImageSHA256,
		outerDeadlineUnixNano, finalDeadlineUnixNano, placeholder, placeholder)
	if err != nil || len(frame) > maxExecutionAuthorizationHandoffFrameBytes {
		return executionAuthorizationHandoffProjection{}, errExecutionAuthorization
	}
	return executionAuthorizationHandoffProjection{frameBytes: uint64(len(frame)), frameSHA256: executionAuthorizationSHA256(frame)}, nil
}

func buildExecutionAuthorizationHandoff(
	executePath, socketPath, executeImageSHA256 string,
	outerDeadlineUnixNano, finalDeadlineUnixNano int64,
	freezeSHA256, sessionBindingSHA256 string,
	projection executionAuthorizationHandoffProjection,
) (executionAuthorizationHandoff, error) {
	wantProjection, err := projectExecutionAuthorizationHandoff(executePath, socketPath, executeImageSHA256,
		outerDeadlineUnixNano, finalDeadlineUnixNano)
	if err != nil || projection != wantProjection || projection.frameBytes == 0 || projection.frameBytes > maxExecutionAuthorizationHandoffFrameBytes ||
		!validExecutionHexSHA256(projection.frameSHA256) {
		return executionAuthorizationHandoff{}, errExecutionAuthorization
	}
	value, authorization, frame, err := assembleExecutionAuthorizationHandoff(executePath, socketPath, executeImageSHA256,
		outerDeadlineUnixNano, finalDeadlineUnixNano, freezeSHA256, sessionBindingSHA256)
	if err != nil || uint64(len(frame)) > projection.frameBytes || len(frame) > maxExecutionAuthorizationHandoffFrameBytes {
		return executionAuthorizationHandoff{}, errExecutionAuthorization
	}
	decoded, err := decodeExecutionAuthorizationHandoff(frame)
	if err != nil || !equalExecutionAuthorizationHandoff(decoded, value) {
		return executionAuthorizationHandoff{}, errExecutionAuthorization
	}
	return executionAuthorizationHandoff{
		value: value, authorization: authorization, frame: frame, frameSHA256: executionAuthorizationSHA256(frame),
	}, nil
}

func assembleExecutionAuthorizationHandoff(
	executePath, socketPath, executeImageSHA256 string,
	outerDeadlineUnixNano, finalDeadlineUnixNano int64,
	freezeSHA256, sessionBindingSHA256 string,
) (executionAuthorizationHandoffV1, []byte, []byte, error) {
	authorization, err := canonicalExecutionAuthorization(executionAuthorizationV1{
		Schema: executionAuthorizationSchema, FreezeSHA256: freezeSHA256, SessionBindingSHA256: sessionBindingSHA256,
	})
	if err != nil || !validExecutionAuthorizationHandoffInputs(executePath, socketPath, executeImageSHA256, outerDeadlineUnixNano, finalDeadlineUnixNano) {
		return executionAuthorizationHandoffV1{}, nil, nil, errExecutionAuthorization
	}
	payload := base64.RawURLEncoding.EncodeToString(authorization)
	if len(payload) != base64.RawURLEncoding.EncodedLen(len(authorization)) {
		return executionAuthorizationHandoffV1{}, nil, nil, errExecutionAuthorization
	}
	argv := []string{executePath, executionAuthorizationMode, "--socket", socketPath, "--payload-base64url", payload}
	argvRaw, err := json.Marshal(argv)
	argvBound, argvBoundErr := checkedExecutionAuthorizationArgvBytes(argv)
	if err != nil || argvBoundErr != nil || argvBound != uint64(len(argvRaw)) {
		return executionAuthorizationHandoffV1{}, nil, nil, errExecutionAuthorization
	}
	rendered, err := renderExecutionAuthorizationCommand(argv)
	renderedBound, renderedBoundErr := checkedExecutionAuthorizationRenderedBytes(argv)
	if err != nil || renderedBoundErr != nil || renderedBound != uint64(len(rendered)) {
		return executionAuthorizationHandoffV1{}, nil, nil, errExecutionAuthorization
	}
	parsed, err := parseExecutionAuthorizationCommand(rendered)
	if err != nil || !equalExecutionAuthorizationArgv(parsed, argv) {
		return executionAuthorizationHandoffV1{}, nil, nil, errExecutionAuthorization
	}
	value := executionAuthorizationHandoffV1{
		Schema: executionAuthorizationHandoffSchema, SocketPath: socketPath,
		AuthorizationJSON: string(authorization), AuthorizationSHA256: executionAuthorizationSHA256(authorization),
		PayloadBase64URL: payload, SessionBindingSHA256: sessionBindingSHA256, FreezeSHA256: freezeSHA256,
		T422ExecuteImageSHA256: executeImageSHA256, OuterDeadlineUnixNano: outerDeadlineUnixNano,
		FinalAdmissionDeadlineUnixNano: finalDeadlineUnixNano, ClientArgv: argv,
		ClientArgvSHA256: executionAuthorizationSHA256(argvRaw), RenderedClientCommand: rendered,
		RenderedClientCommandSHA256: executionAuthorizationSHA256([]byte(rendered)),
	}
	if err := validateExecutionAuthorizationHandoff(value); err != nil {
		return executionAuthorizationHandoffV1{}, nil, nil, err
	}
	raw, err := json.Marshal(value)
	bound, boundErr := checkedExecutionAuthorizationHandoffBytes(value)
	if err != nil || boundErr != nil || bound != uint64(len(raw)) {
		return executionAuthorizationHandoffV1{}, nil, nil, errExecutionAuthorization
	}
	frameBound := bound
	if !addExecutionAuthorizationBytes(&frameBound, 1) || frameBound > maxExecutionAuthorizationHandoffFrameBytes {
		return executionAuthorizationHandoffV1{}, nil, nil, errExecutionAuthorization
	}
	frame := append(append(make([]byte, 0, int(frameBound)), raw...), '\n')
	if uint64(len(frame)) != frameBound {
		return executionAuthorizationHandoffV1{}, nil, nil, errExecutionAuthorization
	}
	return value, authorization, frame, nil
}

func decodeExecutionAuthorizationHandoff(frame []byte) (executionAuthorizationHandoffV1, error) {
	var value executionAuthorizationHandoffV1
	if len(frame) < 2 || len(frame) > maxExecutionAuthorizationHandoffFrameBytes || frame[len(frame)-1] != '\n' ||
		bytes.Count(frame, []byte{'\n'}) != 1 {
		return value, errExecutionAuthorization
	}
	raw := frame[:len(frame)-1]
	if decodeExecutionAuthorizationCanonical(raw, &value) != nil || validateExecutionAuthorizationHandoff(value) != nil {
		return executionAuthorizationHandoffV1{}, errExecutionAuthorization
	}
	canonical, err := json.Marshal(value)
	bound, boundErr := checkedExecutionAuthorizationHandoffBytes(value)
	if err != nil || boundErr != nil || bound != uint64(len(raw)) || !bytes.Equal(canonical, raw) {
		return executionAuthorizationHandoffV1{}, errExecutionAuthorization
	}
	return value, nil
}

func validateExecutionAuthorizationHandoff(value executionAuthorizationHandoffV1) error {
	if value.Schema != executionAuthorizationHandoffSchema ||
		!validExecutionAuthorizationHandoffInputs(value.clientArgvPath(), value.SocketPath, value.T422ExecuteImageSHA256,
			value.OuterDeadlineUnixNano, value.FinalAdmissionDeadlineUnixNano) ||
		!validExecutionHexSHA256(value.AuthorizationSHA256) || !validExecutionHexSHA256(value.SessionBindingSHA256) ||
		!validExecutionHexSHA256(value.FreezeSHA256) || !validExecutionHexSHA256(value.ClientArgvSHA256) ||
		!validExecutionHexSHA256(value.RenderedClientCommandSHA256) {
		return errExecutionAuthorization
	}
	authorization := []byte(value.AuthorizationJSON)
	decoded, err := decodeExecutionAuthorization(authorization)
	if err != nil || decoded.FreezeSHA256 != value.FreezeSHA256 || decoded.SessionBindingSHA256 != value.SessionBindingSHA256 ||
		executionAuthorizationSHA256(authorization) != value.AuthorizationSHA256 ||
		base64.RawURLEncoding.EncodeToString(authorization) != value.PayloadBase64URL {
		return errExecutionAuthorization
	}
	wantArgv := []string{value.clientArgvPath(), executionAuthorizationMode, "--socket", value.SocketPath, "--payload-base64url", value.PayloadBase64URL}
	if !equalExecutionAuthorizationArgv(value.ClientArgv, wantArgv) {
		return errExecutionAuthorization
	}
	argvRaw, err := json.Marshal(value.ClientArgv)
	argvBound, argvBoundErr := checkedExecutionAuthorizationArgvBytes(value.ClientArgv)
	rendered, renderErr := renderExecutionAuthorizationCommand(value.ClientArgv)
	renderedBound, renderedBoundErr := checkedExecutionAuthorizationRenderedBytes(value.ClientArgv)
	parsed, parseErr := parseExecutionAuthorizationCommand(value.RenderedClientCommand)
	if err != nil || argvBoundErr != nil || argvBound != uint64(len(argvRaw)) || renderErr != nil || renderedBoundErr != nil ||
		renderedBound != uint64(len(rendered)) || parseErr != nil || executionAuthorizationSHA256(argvRaw) != value.ClientArgvSHA256 ||
		rendered != value.RenderedClientCommand || executionAuthorizationSHA256([]byte(rendered)) != value.RenderedClientCommandSHA256 ||
		!equalExecutionAuthorizationArgv(parsed, value.ClientArgv) {
		return errExecutionAuthorization
	}
	return nil
}

func (value executionAuthorizationHandoffV1) clientArgvPath() string {
	if len(value.ClientArgv) != 6 {
		return ""
	}
	return value.ClientArgv[0]
}

func validExecutionAuthorizationHandoffInputs(executePath, socketPath, executeImageSHA256 string, outerDeadlineUnixNano, finalDeadlineUnixNano int64) bool {
	return validExecutionLauncherPath(executePath) && utf8.ValidString(executePath) && !strings.ContainsRune(executePath, '\n') &&
		validExecutionAuthorizationSocketPath(socketPath) && !strings.ContainsRune(socketPath, '\n') &&
		validExecutionSHA256(executeImageSHA256) && outerDeadlineUnixNano > 0 && finalDeadlineUnixNano > 0 &&
		finalDeadlineUnixNano <= outerDeadlineUnixNano
}

func checkedExecutionAuthorizationHandoffBytes(value executionAuthorizationHandoffV1) (uint64, error) {
	fields := []struct {
		name  string
		value any
	}{
		{"schema", value.Schema}, {"socket_path", value.SocketPath}, {"authorization_json", value.AuthorizationJSON},
		{"authorization_sha256", value.AuthorizationSHA256}, {"payload_base64url", value.PayloadBase64URL},
		{"session_binding_sha256", value.SessionBindingSHA256}, {"freeze_sha256", value.FreezeSHA256},
		{"t422_execute_image_sha256", value.T422ExecuteImageSHA256}, {"outer_deadline_unix_nano", value.OuterDeadlineUnixNano},
		{"final_admission_deadline_unix_nano", value.FinalAdmissionDeadlineUnixNano}, {"client_argv", value.ClientArgv},
		{"client_argv_sha256", value.ClientArgvSHA256}, {"rendered_client_command", value.RenderedClientCommand},
		{"rendered_client_command_sha256", value.RenderedClientCommandSHA256},
	}
	total := uint64(2) // braces
	for index, field := range fields {
		name, nameErr := json.Marshal(field.name)
		encoded, valueErr := json.Marshal(field.value)
		if nameErr != nil || valueErr != nil || !addExecutionAuthorizationBytes(&total, len(name), 1, len(encoded)) ||
			index > 0 && !addExecutionAuthorizationBytes(&total, 1) {
			return 0, errExecutionAuthorization
		}
	}
	return total, nil
}

func addExecutionAuthorizationBytes(total *uint64, lengths ...int) bool {
	if total == nil {
		return false
	}
	for _, length := range lengths {
		if length < 0 || uint64(length) > math.MaxUint64-*total {
			return false
		}
		*total += uint64(length)
	}
	return true
}

func checkedExecutionAuthorizationArgvBytes(argv []string) (uint64, error) {
	if len(argv) != 6 {
		return 0, errExecutionAuthorization
	}
	total := uint64(2) // brackets
	for index, argument := range argv {
		encoded, err := json.Marshal(argument)
		if err != nil || !addExecutionAuthorizationBytes(&total, len(encoded)) || index > 0 && !addExecutionAuthorizationBytes(&total, 1) {
			return 0, errExecutionAuthorization
		}
	}
	return total, nil
}

func checkedExecutionAuthorizationRenderedBytes(argv []string) (uint64, error) {
	if len(argv) != 6 {
		return 0, errExecutionAuthorization
	}
	total := uint64(0)
	for index, argument := range argv {
		if !utf8.ValidString(argument) || strings.ContainsRune(argument, 0) || strings.ContainsRune(argument, '\n') ||
			!addExecutionAuthorizationBytes(&total, 2, len(argument), 3*strings.Count(argument, "'")) ||
			index > 0 && !addExecutionAuthorizationBytes(&total, 1) {
			return 0, errExecutionAuthorization
		}
	}
	return total, nil
}

func renderExecutionAuthorizationCommand(argv []string) (string, error) {
	if len(argv) != 6 {
		return "", errExecutionAuthorization
	}
	quoted := make([]string, len(argv))
	for index, argument := range argv {
		if !utf8.ValidString(argument) || strings.ContainsRune(argument, 0) || strings.ContainsRune(argument, '\n') {
			return "", errExecutionAuthorization
		}
		quoted[index] = "'" + strings.ReplaceAll(argument, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " "), nil
}

func parseExecutionAuthorizationCommand(command string) ([]string, error) {
	if command == "" || !utf8.ValidString(command) || strings.ContainsRune(command, 0) || strings.ContainsRune(command, '\n') {
		return nil, errExecutionAuthorization
	}
	var argv []string
	for offset := 0; offset < len(command); {
		if command[offset] != '\'' {
			return nil, errExecutionAuthorization
		}
		offset++
		var argument strings.Builder
		for {
			if offset >= len(command) {
				return nil, errExecutionAuthorization
			}
			if command[offset] != '\'' {
				argument.WriteByte(command[offset])
				offset++
				continue
			}
			if strings.HasPrefix(command[offset:], "'\\''") {
				argument.WriteByte('\'')
				offset += 4
				continue
			}
			offset++
			argv = append(argv, argument.String())
			break
		}
		if offset == len(command) {
			break
		}
		if command[offset] != ' ' || offset+1 >= len(command) || command[offset+1] != '\'' {
			return nil, errExecutionAuthorization
		}
		offset++
	}
	if len(argv) != 6 {
		return nil, errExecutionAuthorization
	}
	return argv, nil
}

func decodeExecutionAuthorizationCanonical(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errExecutionAuthorization
	}
	return nil
}

func executionAuthorizationSHA256(raw []byte) string {
	return strings.TrimPrefix(SHA256(raw), "sha256:")
}

func equalExecutionAuthorizationArgv(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalExecutionAuthorizationHandoff(left, right executionAuthorizationHandoffV1) bool {
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}
