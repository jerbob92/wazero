//go:build !((darwin || linux) && (amd64 || arm64))

package wasm

import (
	"errors"

	"github.com/tetratelabs/wazero/experimental"
)

// newGuardedLinearMemory is unavailable on this platform; the caller falls
// back to a regular heap-allocated memory (and the engine never compiles
// checkless code here, since platform.GuardPageMemorySupported is false).
func newGuardedLinearMemory(uint64) (experimental.LinearMemory, error) {
	return nil, errors.New("guard-page memory is not supported on this platform")
}
