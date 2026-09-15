//go:build !darwin

package t4013

import "errors"

// PrivateProcessSessionMembership requires coherent native Darwin records.
func PrivateProcessSessionMembership(int) ([]NativeProcessRecord, error) {
	return nil, errors.New("native session-member observation requires macOS")
}
