//go:build !cgo || !((darwin || linux) && (amd64 || arm64))

package guardsig

// Supported returns false: without cgo (or on an unsupported platform) the
// fault handler is unavailable, and experimental.WithUncheckedMemoryAccess
// silently falls back to bounds-checked code generation.
func Supported() bool { return false }

// RegisterRegion always fails: no handler is available to register with.
func RegisterRegion(uintptr, uintptr) bool { return false }

// UnregisterRegion is a no-op: no handler is available to unregister from.
func UnregisterRegion(uintptr) {}
