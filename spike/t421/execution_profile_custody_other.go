//go:build !darwin

package t421

// V3 mounted workspace custody is Darwin-only. The empty private type keeps
// the common flow layout buildable without exposing a non-Darwin issuer.
type executionWorkspaceCustodyCapability struct{}
