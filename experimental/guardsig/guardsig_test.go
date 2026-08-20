package guardsig_test

import (
	"testing"

	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/experimental/guardsig"
	"github.com/tetratelabs/wazero/internal/testing/require"
)

// The full end-to-end fault-recovery test (a real wazero runtime, guard-page
// memory, and actual hardware traps) lives in experimental/guardpage, which
// exercises this package together with a real experimental.MemoryAllocator.
// This test only checks the mechanics this package owns directly: region
// registration/deregistration, and that it registers itself as the process
// GuardFaultHandler when its C handler installs successfully.

func TestSupported(t *testing.T) {
	// Supported() reflects whether the C handler installed; on any platform
	// this package builds its real implementation for, that should succeed.
	if !guardsig.Supported() {
		t.Skip("guard-fault handler unavailable on this platform/build")
	}
	require.True(t, experimental.GuardFaultHandlerInstalled())
}

func TestRegisterUnregisterRegion(t *testing.T) {
	if !guardsig.Supported() {
		t.Skip("guard-fault handler unavailable on this platform/build")
	}
	const base, end = 0x1000, 0x2000
	require.True(t, guardsig.RegisterRegion(base, end))
	guardsig.UnregisterRegion(base)
	// Re-registering the same range must succeed again (the slot was freed).
	require.True(t, guardsig.RegisterRegion(base, end))
	guardsig.UnregisterRegion(base)
}
