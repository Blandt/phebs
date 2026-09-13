package t421

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestExecutionAuthorizationSessionBindingIsExactCanonicalPreimage(t *testing.T) {
	value := executionAuthorizationSessionBindingTestValue()
	binding, err := buildExecutionAuthorizationSessionBinding(value)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`{"schema":"t422-execution-authorization-session-binding-v1","ceremony_id":"t422-test","freeze_sha256":"%s","outer_pid":11,"outer_start_token":"1:2","inner_pid":12,"inner_parent_pid":11,"inner_start_token":"3:4","inner_session_id":12,"inner_process_group_id":12,"t422_execute_canonical_path_sha256":"%s","t422_execute_st_dev":1,"t422_execute_st_ino":2,"t422_execute_st_mode":33088,"t422_execute_size":3,"t422_execute_ctime_unix_nano":4,"t422_execute_image_sha256":"sha256:%s","listener_parent_st_dev":5,"listener_parent_st_ino":6,"listener_st_dev":7,"listener_st_ino":8,"listener_st_mode":49536,"socket_path_sha256":"%s","outer_deadline_unix_nano":10,"final_admission_deadline_unix_nano":9}`,
		strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64))
	if string(binding.raw) != want || binding.sha256 != executionAuthorizationSHA256(binding.raw) || len(binding.raw) > maxExecutionAuthorizationSessionBytes {
		t.Fatalf("session binding differs from exact preimage: %q", binding.raw)
	}
	decoded, err := decodeExecutionAuthorizationSessionBinding(binding.raw)
	if err != nil || decoded != value {
		t.Fatalf("session round trip = %+v, %v", decoded, err)
	}
	for _, invalid := range [][]byte{
		append(append([]byte(nil), binding.raw...), '\n'),
		bytes.Replace(binding.raw, []byte(`"schema":`), []byte(`"unknown":0,"schema":`), 1),
		bytes.Replace(binding.raw, []byte(`"outer_pid":11`), []byte(`"outer_pid":011`), 1),
		bytes.Replace(binding.raw, []byte(`"outer_pid":11`), []byte(`"outer_pid":-1`), 1),
		bytes.Replace(binding.raw, []byte(`"schema":"t422-execution-authorization-session-binding-v1","ceremony_id"`), []byte(`"ceremony_id":"t422-test","schema"`), 1),
	} {
		if _, err := decodeExecutionAuthorizationSessionBinding(invalid); err == nil {
			t.Fatalf("invalid session binding admitted: %q", invalid)
		}
	}
}

func TestExecutionAuthorizationHandoffProjectionAndFrameAreExact(t *testing.T) {
	executePath := "/tmp/t422 'execute"
	socketPath := "/tmp/t422 socket/auth.sock"
	image := "sha256:" + strings.Repeat("e", 64)
	projection, err := projectExecutionAuthorizationHandoff(executePath, socketPath, image, 20)
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := buildExecutionAuthorizationHandoff(executePath, socketPath, image, 20, 19,
		strings.Repeat("a", 64), strings.Repeat("b", 64), projection)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(handoff.frame)) > projection.frameBytes || len(handoff.frame) > maxExecutionAuthorizationHandoffFrameBytes ||
		handoff.frame[len(handoff.frame)-1] != '\n' || bytes.Count(handoff.frame, []byte{'\n'}) != 1 ||
		handoff.frameSHA256 != executionAuthorizationSHA256(handoff.frame) {
		t.Fatal("handoff frame does not meet its projected exact bound")
	}
	decoded, err := decodeExecutionAuthorizationHandoff(handoff.frame)
	if err != nil || !equalExecutionAuthorizationHandoff(decoded, handoff.value) {
		t.Fatal("handoff frame did not round trip", err)
	}
	wantArgv := []string{executePath, "authorize-t422", "--socket", socketPath, "--payload-base64url", handoff.value.PayloadBase64URL}
	if !equalExecutionAuthorizationArgv(handoff.value.ClientArgv, wantArgv) || !strings.Contains(handoff.value.RenderedClientCommand, `'\''`) {
		t.Fatalf("client command = %q", handoff.value.RenderedClientCommand)
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(handoff.value.PayloadBase64URL)
	if err != nil || !bytes.Equal(payload, handoff.authorization) || string(payload) != handoff.value.AuthorizationJSON {
		t.Fatal("authorization payload differs from canonical bytes", err)
	}
	argvRaw, _ := json.Marshal(wantArgv)
	if handoff.value.AuthorizationSHA256 != executionAuthorizationSHA256(handoff.authorization) ||
		handoff.value.ClientArgvSHA256 != executionAuthorizationSHA256(argvRaw) ||
		handoff.value.RenderedClientCommandSHA256 != executionAuthorizationSHA256([]byte(handoff.value.RenderedClientCommand)) {
		t.Fatal("handoff digest does not bind its exact source bytes")
	}

	badProjection := projection
	badProjection.frameBytes++
	if _, err := buildExecutionAuthorizationHandoff(executePath, socketPath, image, 20, 19,
		strings.Repeat("a", 64), strings.Repeat("b", 64), badProjection); err == nil {
		t.Fatal("changed preclaim projection was admitted")
	}
	changed := append([]byte(nil), handoff.frame...)
	changed = bytes.Replace(changed, []byte(`"authorization_sha256":"`), []byte(`"unknown":"x","authorization_sha256":"`), 1)
	if _, err := decodeExecutionAuthorizationHandoff(changed); err == nil {
		t.Fatal("unknown handoff field was admitted")
	}
}

func TestExecutionAuthorizationCommandParserIsStrict(t *testing.T) {
	argv := []string{"/tmp/a'b", "authorize-t422", "--socket", "/tmp/auth.sock", "--payload-base64url", "abc"}
	rendered, err := renderExecutionAuthorizationCommand(argv)
	decoded, decodeErr := parseExecutionAuthorizationCommand(rendered)
	if err != nil || decodeErr != nil || !equalExecutionAuthorizationArgv(decoded, argv) {
		t.Fatalf("quoted command round trip = %q, %#v, %v, %v", rendered, decoded, err, decodeErr)
	}
	for _, invalid := range []string{"", rendered + " ", strings.Replace(rendered, " ", "  ", 1), "'unterminated", "'a'\\'b'"} {
		if _, err := parseExecutionAuthorizationCommand(invalid); err == nil {
			t.Fatalf("invalid rendered command admitted: %q", invalid)
		}
	}
}

func executionAuthorizationSessionBindingTestValue() executionAuthorizationSessionBindingPreimageV1 {
	return executionAuthorizationSessionBindingPreimageV1{
		Schema: executionAuthorizationSessionBindingSchema, CeremonyID: "t422-test", FreezeSHA256: strings.Repeat("a", 64),
		OuterPID: 11, OuterStartToken: "1:2", InnerPID: 12, InnerParentPID: 11, InnerStartToken: "3:4",
		InnerSessionID: 12, InnerProcessGroupID: 12,
		T422ExecuteCanonicalPathSHA256: strings.Repeat("b", 64), T422ExecuteDevice: 1, T422ExecuteInode: 2,
		T422ExecuteMode: 0o100500, T422ExecuteSize: 3, T422ExecuteCTimeUnixNano: 4,
		T422ExecuteImageSHA256: "sha256:" + strings.Repeat("c", 64),
		ListenerParentDevice:   5, ListenerParentInode: 6, ListenerDevice: 7, ListenerInode: 8,
		ListenerMode: 0o140600, SocketPathSHA256: strings.Repeat("d", 64),
		OuterDeadlineUnixNano: 10, FinalAdmissionDeadlineUnixNano: 9,
	}
}
