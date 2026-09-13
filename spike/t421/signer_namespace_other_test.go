//go:build !darwin

package t421

import "testing"

func newExecutionSignerNamespaceTestBinding(t *testing.T) executionSignerNamespaceBinding {
	t.Helper()
	t.Skip("live T42.2 signer namespace custody requires Darwin")
	return executionSignerNamespaceBinding{}
}
