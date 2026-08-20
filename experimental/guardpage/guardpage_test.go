package guardpage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/experimental/guardpage"
	"github.com/tetratelabs/wazero/experimental/guardsig"
	"github.com/tetratelabs/wazero/internal/platform"
	"github.com/tetratelabs/wazero/internal/testing/binaryencoding"
	"github.com/tetratelabs/wazero/internal/testing/require"
	"github.com/tetratelabs/wazero/internal/wasm"
	"github.com/tetratelabs/wazero/internal/wasmruntime"
)

// testModule returns a module with a 1-page (max 2) memory exporting:
//   - "load8" (param addr i32) (result i32): i32.load8_u
//   - "load8drop" (param addr i32): i32.load8_u + drop (must still trap)
//   - "grow" (param pages i32) (result i32): memory.grow
func testModule() []byte {
	two := uint32(2)
	m := &wasm.Module{
		TypeSection: []wasm.FunctionType{
			{Params: []wasm.ValueType{wasm.ValueTypeI32}, Results: []wasm.ValueType{wasm.ValueTypeI32}},
			{Params: []wasm.ValueType{wasm.ValueTypeI32}},
		},
		FunctionSection: []wasm.Index{0, 1, 0},
		MemorySection:   &wasm.Memory{Min: 1, Cap: 1, Max: two, IsMaxEncoded: true},
		CodeSection: []wasm.Code{
			{Body: []byte{wasm.OpcodeLocalGet, 0, wasm.OpcodeI32Load8U, 0, 0, wasm.OpcodeEnd}},
			{Body: []byte{wasm.OpcodeLocalGet, 0, wasm.OpcodeI32Load8U, 0, 0, wasm.OpcodeDrop, wasm.OpcodeEnd}},
			{Body: []byte{wasm.OpcodeLocalGet, 0, wasm.OpcodeMemoryGrow, 0, wasm.OpcodeEnd}},
		},
		ExportSection: []wasm.Export{
			{Name: "load8", Type: wasm.ExternTypeFunc, Index: 0},
			{Name: "load8drop", Type: wasm.ExternTypeFunc, Index: 1},
			{Name: "grow", Type: wasm.ExternTypeFunc, Index: 2},
		},
	}
	return binaryencoding.EncodeModule(m)
}

func TestAllocator_traps(t *testing.T) {
	if !platform.CompilerSupported() {
		t.Skip("optimizing compiler unavailable (e.g. exec-mmap not permitted in this sandbox)")
	}
	if !guardpage.Supported || !guardsig.Supported() {
		t.Skip("guard-page reservation or fault handler unavailable")
	}

	// WithUncheckedMemoryAccess is a compile-time flag (passed to
	// NewRuntimeWithConfig); WithMemoryAllocator is an instantiate-time
	// wiring (passed to Instantiate). They are independent: any allocator
	// that produces an UnsafeLinearMemory can be paired with the flag.
	compileCtx := experimental.WithUncheckedMemoryAccess(context.Background())
	r := wazero.NewRuntimeWithConfig(compileCtx, wazero.NewRuntimeConfigCompiler())
	defer r.Close(compileCtx)

	instCtx := experimental.WithMemoryAllocator(compileCtx, guardpage.Allocator)
	mod, err := r.Instantiate(instCtx, testModule())
	require.NoError(t, err)

	load8 := mod.ExportedFunction("load8")

	// In bounds.
	res, err := load8.Call(instCtx, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(0), res[0])
	_, err = load8.Call(instCtx, 65535)
	require.NoError(t, err)

	// Out of bounds: first byte past the memory, and far beyond.
	for _, addr := range []uint64{65536, 65537, 100000, 1 << 20, 0xffffffff} {
		_, err = load8.Call(instCtx, addr)
		require.True(t, errors.Is(err, wasmruntime.ErrRuntimeOutOfBoundsMemoryAccess), "addr=%d err=%v", addr, err)
	}

	// A load whose result is dropped must still trap.
	_, err = mod.ExportedFunction("load8drop").Call(instCtx, 65536)
	require.True(t, errors.Is(err, wasmruntime.ErrRuntimeOutOfBoundsMemoryAccess), "dropped load err=%v", err)

	// Growing commits more of the reservation; the boundary moves.
	res, err = mod.ExportedFunction("grow").Call(instCtx, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), res[0])
	_, err = load8.Call(instCtx, 65536) // now in bounds
	require.NoError(t, err)
	_, err = load8.Call(instCtx, 131072) // new boundary
	require.True(t, errors.Is(err, wasmruntime.ErrRuntimeOutOfBoundsMemoryAccess), "after grow err=%v", err)

	// The engine still works for subsequent calls after many traps.
	res, err = load8.Call(instCtx, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(0), res[0])
}

// TestAllocator_uncheckedWithoutHandler verifies that
// WithUncheckedMemoryAccess with no experimental.GuardFaultHandler installed
// silently falls back to bounds-checked codegen. Runs whenever the
// optimizing compiler is usable (see platform.CompilerSupported, which
// probes exec-mmap rather than just checking GOARCH — this must be skipped,
// not asserted, in sandboxes that disallow it) and no handler happens to be
// installed in this test binary, including with cgo disabled.
func TestAllocator_uncheckedWithHandlerAbsent(t *testing.T) {
	if !platform.CompilerSupported() {
		t.Skip("optimizing compiler unavailable (e.g. exec-mmap not permitted in this sandbox)")
	}
	if experimental.GuardFaultHandlerInstalled() {
		t.Skip("a GuardFaultHandler is installed in this test binary (guardsig linked and functional); nothing to verify here")
	}
	ctx := experimental.WithUncheckedMemoryAccess(context.Background())
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
	defer r.Close(ctx)

	// No allocator at all: an ordinary heap-backed memory. Since no
	// GuardFaultHandler is registered, the engine must have silently
	// compiled bounds-checked code, so this must trap normally (not crash
	// the process).
	mod, err := r.Instantiate(ctx, testModule())
	require.NoError(t, err)
	_, err = mod.ExportedFunction("load8").Call(ctx, 65536)
	require.True(t, errors.Is(err, wasmruntime.ErrRuntimeOutOfBoundsMemoryAccess))
}
