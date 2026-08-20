package v2

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/experimental/guardpage"
	"github.com/tetratelabs/wazero/experimental/guardsig"
	"github.com/tetratelabs/wazero/internal/integration_test/spectest"
	"github.com/tetratelabs/wazero/internal/platform"
)

const enabledFeatures = api.CoreFeaturesV2

func TestCompiler(t *testing.T) {
	if !platform.CompilerSupported() {
		t.Skip()
	}
	spectest.Run(t, Testcases, context.Background(), wazero.NewRuntimeConfigCompiler().WithCoreFeatures(enabledFeatures))
}

func TestInterpreter(t *testing.T) {
	spectest.Run(t, Testcases, context.Background(), wazero.NewRuntimeConfigInterpreter().WithCoreFeatures(enabledFeatures))
}

// TestCompilerUncheckedMemory runs the whole suite with per-access memory
// bounds checks disabled (experimental.WithUncheckedMemoryAccess), backed by
// the reference guard-page allocator (experimental/guardpage) and fault
// handler (experimental/guardsig); every out-of-bounds trap assertion must
// still hold, now via the hardware fault path redirecting into the compiled
// guard-fault exit sequence.
func TestCompilerUncheckedMemory(t *testing.T) {
	if !platform.CompilerSupported() || !guardpage.Supported || !guardsig.Supported() {
		t.Skip()
	}
	ctx := experimental.WithUncheckedMemoryAccess(context.Background())
	ctx = experimental.WithMemoryAllocator(ctx, guardpage.Allocator)
	spectest.Run(t, Testcases, ctx, wazero.NewRuntimeConfigCompiler().WithCoreFeatures(enabledFeatures))
}
