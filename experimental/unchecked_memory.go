package experimental

import (
	"context"

	"github.com/tetratelabs/wazero/internal/expctxkeys"
)

// WithUncheckedMemoryAccess instructs wazevo's optimizing compiler to omit
// explicit per-access bounds checks on plain linear-memory loads and stores.
//
// This is unsafe on its own: without a check, an out-of-bounds guest access
// reads or corrupts unrelated memory instead of trapping. It is only safe to
// enable when every memory instance the resulting compiled module runs
// against is backed by a LinearMemory that implements UnsafeLinearMemory,
// i.e. one that independently guarantees any address wazevo could compute
// (u32 base + u32 static offset + access size) either lands within
// allocated memory or is recoverable back to the standard wasm
// out-of-bounds trap through a registered GuardFaultHandler. Pairing a
// module compiled with this flag against a memory that does not implement
// UnsafeLinearMemory fails instantiation rather than running unsafely.
//
// wazero ships no such LinearMemory or GuardFaultHandler implementation
// itself — this is purely a compiler flag and an extension point. See
// experimental/guardpage for a reference LinearMemory/MemoryAllocator (a
// virtual-memory guard-page reservation) and experimental/guardsig for a
// reference GuardFaultHandler (a cgo hardware-fault handler), which
// together let wazero omit bounds checks the same way wasmtime and V8 do.
// Both are optional and unimported by default: wazero's core module has no
// cgo and no additional dependencies regardless of whether this flag is
// used.
//
// Notes:
//   - This context must be passed to wazero.NewRuntimeWithConfig: it is a
//     compile-time flag for the optimizing compiler, decided once when the
//     engine is created, not per module instantiation.
//   - If no GuardFaultHandler is registered (see SetGuardFaultHandler) when
//     the engine is created, this flag is silently ignored and the compiler
//     generates ordinary bounds-checked code.
//   - Only supported by the optimizing compiler (wazevo) on amd64/arm64.
//   - Shared (threads) memories are not supported and always compile with
//     bounds checks.
func WithUncheckedMemoryAccess(ctx context.Context) context.Context {
	return context.WithValue(ctx, expctxkeys.UncheckedMemoryAccessKey{}, true)
}

// UnsafeLinearMemory is an optional interface a LinearMemory may implement
// to declare that it is safe to pair with WithUncheckedMemoryAccess. See
// WithUncheckedMemoryAccess for the exact contract implementations must
// uphold.
type UnsafeLinearMemory interface {
	LinearMemory

	// IsUnsafeLinearMemory marks the type; implementations should always
	// return true (a plain type-assertion to this interface has no other
	// signal to test).
	IsUnsafeLinearMemory() bool
}

// GuardFaultHandler is implemented by an external package that installs a
// hardware fault handler capable of redirecting a thread that faulted on an
// UnsafeLinearMemory access back into wazero's compiled trap-exit sequence,
// so the fault is reported as the standard wasm out-of-bounds error instead
// of crashing the process.
//
// wazero calls RegisterCallStack/UnregisterCallStack whenever it creates,
// grows, or restores a call-engine stack that may execute code compiled
// under WithUncheckedMemoryAccess, so the handler can resolve a faulting
// stack pointer back to the right execution context and resume address.
// wazero has no default implementation of this interface: without one
// registered, WithUncheckedMemoryAccess has no effect. See
// experimental/guardsig for a reference implementation.
//
// Classifying whether a fault address itself falls inside memory that
// should be treated as a guest out-of-bounds access (as opposed to an
// unrelated crash) is entirely the concern of the UnsafeLinearMemory/
// GuardFaultHandler pair in use and is deliberately not part of this
// interface: wazero only needs to help resolve *which* live call, if any,
// a fault occurred on.
type GuardFaultHandler interface {
	// RegisterCallStack registers the [lo, hi) address range of a call
	// engine's stack together with the execution context pointer and the
	// address of the compiled guard-fault exit sequence to resume at on a
	// match. Returns false if the handler cannot accept the registration
	// (e.g. a fixed-size table is full), in which case the caller must not
	// proceed with unchecked code on that stack.
	RegisterCallStack(lo, hi, execCtx, exitSeq uintptr) bool
	// UnregisterCallStack removes a previously registered stack.
	UnregisterCallStack(lo uintptr)
}

var guardFaultHandler GuardFaultHandler

// SetGuardFaultHandler installs the process-wide GuardFaultHandler used by
// WithUncheckedMemoryAccess. Typically called once, from an external
// package's init function, before any Runtime using
// WithUncheckedMemoryAccess is created. Calling it again replaces the
// previous handler.
func SetGuardFaultHandler(h GuardFaultHandler) {
	guardFaultHandler = h
}

// GuardFaultHandlerInstalled reports whether a GuardFaultHandler is
// currently registered. WithUncheckedMemoryAccess has no effect until one
// is.
func GuardFaultHandlerInstalled() bool {
	return guardFaultHandler != nil
}

// RegisterGuardCallStack forwards to the registered GuardFaultHandler, or is
// a no-op (reporting success) if none is registered. Used internally by
// wazero's optimizing compiler.
func RegisterGuardCallStack(lo, hi, execCtx, exitSeq uintptr) bool {
	if guardFaultHandler == nil {
		return true
	}
	return guardFaultHandler.RegisterCallStack(lo, hi, execCtx, exitSeq)
}

// UnregisterGuardCallStack forwards to the registered GuardFaultHandler, or
// is a no-op if none is registered. Used internally by wazero's optimizing
// compiler.
func UnregisterGuardCallStack(lo uintptr) {
	if guardFaultHandler != nil {
		guardFaultHandler.UnregisterCallStack(lo)
	}
}
