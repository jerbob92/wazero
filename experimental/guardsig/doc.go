// Package guardsig is a reference experimental.GuardFaultHandler: a
// cgo-based hardware-fault handler that lets wazero's optimizing compiler
// omit per-access linear-memory bounds checks (see
// experimental.WithUncheckedMemoryAccess) when paired with a LinearMemory
// that implements experimental.UnsafeLinearMemory, such as the one provided
// by experimental/guardpage.
//
// The Go runtime cannot recover a fault raised at a JIT program counter, so
// this package installs a C signal handler via a Go init function that runs
// after the Go runtime has installed its own handlers. It becomes the
// primary handler and chains to the previously installed (Go runtime) one
// for every fault that is not resolved by a registered call stack, which is
// the interposition pattern documented in the os/signal package. On a
// match, it rewrites the faulting thread's saved program counter to the
// compiled exit sequence, with the execution context pointer and the
// faulting PC placed in ISA-specific registers, and resumes; the process
// never re-enters Go from within the signal handler.
//
// This package knows nothing about guard pages, memory reservations, or any
// specific LinearMemory implementation: it only implements the
// stack-registration half of the fault-handling contract
// (experimental.GuardFaultHandler) plus RegisterRegion/UnregisterRegion, a
// small address-range classifier that any LinearMemory/MemoryAllocator
// implementation can call directly to have its reserved-but-inaccessible
// regions recognized by the handler. experimental/guardpage is one such
// caller; nothing prevents another allocator from using this same handler.
//
// Usage: blank-import this package (or otherwise ensure its init function
// runs) and enable unchecked memory access:
//
//	import _ "github.com/tetratelabs/wazero/experimental/guardsig"
//
//	ctx := experimental.WithUncheckedMemoryAccess(context.Background())
//	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
//
// When this package is not imported, or cgo is disabled, or the platform is
// unsupported, WithUncheckedMemoryAccess silently degrades to the regular
// bounds-checked code generation: programs behave identically either way.
//
// Importing this package makes the enclosing program a cgo program.
// wazero's core module remains pure Go regardless.
package guardsig
