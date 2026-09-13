//go:build !darwin

package t421

// Common flow cleanup retains the shared namespace holder; unsupported
// platforms have no native profile or execution-admission state.
type executionEpochPlatform struct {
	profileSignerNamespace *executionSignerNamespaceCustody
}
