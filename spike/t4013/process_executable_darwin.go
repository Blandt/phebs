//go:build darwin

package t4013

import (
	"bytes"
	"encoding/binary"
	"errors"
	"path/filepath"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const maxProcessPath = 1_024
const maxProcessArguments = 1 << 20

func processExecutablePaths(pids []int) ([]string, error) {
	argumentMaximum, err := unix.SysctlUint32("kern.argmax")
	if err != nil || !validProcessArgumentMaximum(argumentMaximum) {
		return nil, errors.Join(err, errors.New("native process executable path is unavailable"))
	}
	paths := make([]string, 0, len(pids))
	for _, pid := range pids {
		buffer, err := unix.SysctlRaw("kern.procargs2", pid)
		if err != nil || len(buffer) > int(argumentMaximum) {
			return nil, errors.Join(err, errors.New("native process executable path is unavailable"))
		}
		path, err := parseProcessExecutablePath(buffer)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func validProcessArgumentMaximum(value uint32) bool {
	return value > 0 && value <= maxProcessArguments
}

func parseProcessExecutablePath(buffer []byte) (string, error) {
	if len(buffer) < 6 || int32(binary.NativeEndian.Uint32(buffer[:4])) <= 0 {
		return "", errors.New("native process executable path is unavailable")
	}
	buffer = buffer[4:]
	end := bytes.IndexByte(buffer, 0)
	if end <= 0 || end >= maxProcessPath {
		return "", errors.New("native process executable path is invalid")
	}
	path := string(buffer[:end])
	if path == "" || len(path) > 1_023 || !utf8.ValidString(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("native process executable path is invalid")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || len(canonical) > 1_023 || !filepath.IsAbs(canonical) || filepath.Clean(canonical) != canonical {
		return "", errors.New("native process executable path is not canonical")
	}
	return canonical, nil
}
