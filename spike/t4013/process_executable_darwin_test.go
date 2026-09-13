//go:build darwin

package t4013

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObserveProcessExecutablePathMatchesCurrentImage(t *testing.T) {
	want, err := os.Executable()
	if err == nil {
		want, err = filepath.EvalSymlinks(want)
	}
	got, gotErr := ObserveProcessExecutablePath(t.Context(), os.Getpid())
	if err != nil || gotErr != nil || got != want {
		t.Fatalf("process executable path = %q, %v; want %q, %v", got, gotErr, want, err)
	}
}

func TestProcessExecutableParserBoundsSignedArgcAndPath(t *testing.T) {
	valid := make([]byte, 4)
	binary.NativeEndian.PutUint32(valid, 1)
	valid = append(valid, []byte("/bin/test\x00ignored")...)
	if path, err := parseProcessExecutablePath(valid); err != nil || path != "/bin/test" {
		t.Fatalf("valid path = %q, %v", path, err)
	}
	for _, raw := range [][]byte{
		nil,
		append([]byte{0, 0, 0, 0}, []byte("/bin/test\x00")...),
		append([]byte{0xff, 0xff, 0xff, 0xff}, []byte("/bin/test\x00")...),
		append([]byte{1, 0, 0, 0}, []byte("relative\x00")...),
		append([]byte{1, 0, 0, 0}, []byte("/"+strings.Repeat("x", maxProcessPath)+"\x00")...),
	} {
		if _, err := parseProcessExecutablePath(raw); err == nil {
			t.Fatal("invalid native argv record admitted")
		}
	}
	for _, value := range []uint32{0, maxProcessArguments + 1} {
		if validProcessArgumentMaximum(value) {
			t.Fatalf("invalid kern.argmax admitted: %d", value)
		}
	}
	if !validProcessArgumentMaximum(maxProcessArguments) {
		t.Fatal("maximum bounded kern.argmax refused")
	}
}
