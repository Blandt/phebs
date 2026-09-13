//go:build darwin

package t421

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestExecutionAuthorizationCanonical(t *testing.T) {
	value := executionAuthorizationTestValue("a", "b")
	raw, err := canonicalExecutionAuthorization(value)
	want := `{"schema":"t422-execution-authorization-v1","freeze_sha256":"` + strings.Repeat("a", 64) + `","session_binding_sha256":"` + strings.Repeat("b", 64) + `"}`
	if err != nil || string(raw) != want || len(raw) > maxExecutionAuthorizationBytes {
		t.Fatalf("canonical authorization = %q, %v", raw, err)
	}
	decoded, err := decodeExecutionAuthorization(raw)
	if err != nil || decoded != value {
		t.Fatalf("decoded authorization = %+v, %v", decoded, err)
	}
	for _, invalid := range [][]byte{
		nil,
		[]byte(`{}`),
		[]byte(`{"freeze_sha256":"` + strings.Repeat("a", 64) + `","schema":"t422-execution-authorization-v1","session_binding_sha256":"` + strings.Repeat("b", 64) + `"}`),
		append(append([]byte(nil), raw...), ' '),
		[]byte(strings.Replace(string(raw), `}`, `,"unknown":"x"}`, 1)),
		[]byte(strings.Replace(string(raw), executionAuthorizationSchema, "wrong", 1)),
		[]byte(strings.Replace(string(raw), strings.Repeat("a", 64), strings.Repeat("A", 64), 1)),
	} {
		if _, err := decodeExecutionAuthorization(invalid); !errors.Is(err, errExecutionAuthorization) {
			t.Fatalf("invalid authorization admitted: %q, %v", invalid, err)
		}
	}
}

func TestExecutionAuthorizationSocketSuccess(t *testing.T) {
	root := executionAuthorizationTestRoot(t)
	deadline := time.Now().Add(5 * time.Second)
	wait, err := prepareExecutionAuthorization(context.Background(), root, deadline)
	if err != nil {
		t.Fatal(err)
	}
	value := executionAuthorizationTestValue("a", "b")
	raw, err := canonicalExecutionAuthorization(value)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(wait.path)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 || filepath.Dir(wait.path) != root.path {
		t.Fatalf("socket custody = %v, %v", info, err)
	}
	flags, err := unix.FcntlInt(uintptr(wait.listener), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("listener flags = %d, %v", flags, err)
	}
	type outcome struct {
		peer executionAuthorizationPeer
		err  error
	}
	result := make(chan outcome, 1)
	go func() {
		peer, err := wait.consume(context.Background(), value)
		result <- outcome{peer: peer, err: err}
	}()
	sent := make(chan error, 1)
	go func() { sent <- sendExecutionAuthorization(context.Background(), wait.path, raw, deadline) }()
	got := <-result
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if got.err != nil || got.peer.pid != os.Getpid() || got.peer.uid != uint32(os.Geteuid()) ||
		!got.peer.listenerCloseOnExec || !got.peer.connectionCloseOnExec {
		t.Fatalf("peer evidence = %+v, %v", got.peer, got.err)
	}
	if _, err := os.Lstat(wait.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("authorization socket survived: %v", err)
	}
	if _, err := wait.consume(context.Background(), value); !errors.Is(err, errExecutionAuthorization) {
		t.Fatalf("spent authorization reused: %v", err)
	}
}

func TestExecutionAuthorizationFirstConnectionSpends(t *testing.T) {
	value := executionAuthorizationTestValue("a", "b")
	raw, err := canonicalExecutionAuthorization(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		frame []byte
		hold  bool
	}{
		{name: "eof_before_lf", frame: raw},
		{name: "trailing", frame: append(append(append([]byte(nil), raw...), '\n'), 'x')},
		{name: "overflow", frame: bytes.Repeat([]byte{'x'}, maxExecutionAuthorizationReadBytes)},
		{name: "noncanonical", frame: append([]byte(`{ "schema":"t422-execution-authorization-v1"}`), '\n')},
		{name: "mismatch", frame: executionAuthorizationTestFrame(t, executionAuthorizationTestValue("c", "b"))},
		{name: "delayed_eof", frame: append(append([]byte(nil), raw...), '\n'), hold: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := executionAuthorizationTestRoot(t)
			deadline := time.Now().Add(500 * time.Millisecond)
			wait, err := prepareExecutionAuthorization(context.Background(), root, deadline)
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := wait.consume(context.Background(), value)
				result <- err
			}()
			connection := executionAuthorizationRawClient(t, wait.path, deadline)
			if _, err := connection.Write(test.frame); err != nil && test.name != "overflow" {
				t.Fatal(err)
			}
			if !test.hold {
				if err := connection.CloseWrite(); err != nil {
					t.Fatal(err)
				}
				_ = connection.Close()
			}
			if err := <-result; !errors.Is(err, errExecutionAuthorization) {
				t.Fatalf("invalid first connection = %v", err)
			}
			_ = connection.Close()
			if _, err := os.Lstat(wait.path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("spent listener survived: %v", err)
			}
			if err := sendExecutionAuthorization(context.Background(), wait.path, raw, time.Now().Add(time.Second)); !errors.Is(err, errExecutionAuthorization) {
				t.Fatalf("invalid first allowed retry: %v", err)
			}
		})
	}
}

func TestExecutionAuthorizationCancellationExpiryAndGuardedUnlink(t *testing.T) {
	for _, mode := range []string{"cancel", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			root := executionAuthorizationTestRoot(t)
			deadline := time.Now().Add(5 * time.Second)
			if mode == "expiry" {
				deadline = time.Now().Add(100 * time.Millisecond)
			}
			wait, err := prepareExecutionAuthorization(context.Background(), root, deadline)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			go func() {
				_, err := wait.consume(ctx, executionAuthorizationTestValue("a", "b"))
				result <- err
			}()
			if mode == "cancel" {
				cancel()
			} else {
				defer cancel()
			}
			select {
			case err := <-result:
				if !errors.Is(err, errExecutionAuthorization) {
					t.Fatalf("%s result = %v", mode, err)
				}
			case <-time.After(time.Second):
				t.Fatalf("%s did not interrupt accept", mode)
			}
			if _, err := os.Lstat(wait.path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s socket survived: %v", mode, err)
			}
		})
	}

	root := executionAuthorizationTestRoot(t)
	wait, err := prepareExecutionAuthorization(context.Background(), root, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(root.path, "held.sock")
	if err := os.Rename(wait.path, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(moved) })
	if err := os.WriteFile(wait.path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := wait.close(); !errors.Is(err, errExecutionAuthorization) {
		t.Fatalf("replaced socket cleanup = %v", err)
	}
	if raw, err := os.ReadFile(wait.path); err != nil || string(raw) != "replacement" {
		t.Fatalf("guarded unlink removed replacement: %q, %v", raw, err)
	}
}

func executionAuthorizationTestValue(freeze, session string) executionAuthorizationV1 {
	return executionAuthorizationV1{
		Schema: executionAuthorizationSchema, FreezeSHA256: strings.Repeat(freeze, 64),
		SessionBindingSHA256: strings.Repeat(session, 64),
	}
}

func executionAuthorizationTestFrame(t *testing.T, value executionAuthorizationV1) []byte {
	t.Helper()
	raw, err := canonicalExecutionAuthorization(value)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func executionAuthorizationTestRoot(t *testing.T) productionRoot {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "t422-auth-")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	directory = canonical
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := openProductionRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = root.file.Close()
		_ = os.RemoveAll(directory)
	})
	return root
}

func executionAuthorizationRawClient(t *testing.T, path string, deadline time.Time) *net.UnixConn {
	t.Helper()
	dialer := net.Dialer{Deadline: deadline}
	connection, err := dialer.DialContext(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		_ = connection.Close()
		t.Fatal("Unix connection was not returned")
	}
	if err := unixConnection.SetDeadline(deadline); err != nil {
		_ = unixConnection.Close()
		t.Fatal(err)
	}
	return unixConnection
}
