// Package guardpage is a reference experimental.MemoryAllocator: it backs a
// wasm linear memory with a large PROT_NONE virtual-memory reservation
// (covering the whole 32-bit wasm address space plus the maximum static
// offset) whose committed prefix grows via mprotect on memory.grow. Any
// dynamic access wazevo's optimizing compiler could compute (u32 base +
// u32 static offset + access size) lands inside the reservation, so an
// out-of-bounds guest access faults in hardware instead of requiring an
// explicit per-access bounds check — the same virtual-memory technique
// wasmtime and V8 use.
//
// This package only allocates and reserves memory; it has no opinion on
// signal handling and needs no cgo of its own (reservation and growth use
// only the standard "syscall" package). To have a fault inside its
// reservation actually redirected back into wazero's compiled trap-exit
// sequence — which is what makes it safe to pair with
// experimental.WithUncheckedMemoryAccess — a experimental.GuardFaultHandler
// must be registered, and this package's reserved regions must be given to
// it for fault classification. For now, this package calls directly into
// experimental/guardsig (this repository's reference handler) to do both:
// a production split into fully independent, separately versioned modules
// would instead have the allocator publish its regions through some future
// shared registration surface that any handler could consume, but for this
// in-tree prototype a direct dependency keeps the two packages easy to
// review together.
//
// Usage:
//
//	ctx := experimental.WithUncheckedMemoryAccess(context.Background())
//	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
//	instCtx := experimental.WithMemoryAllocator(ctx, guardpage.Allocator)
//	mod, err := r.InstantiateWithConfig(instCtx, wasmBin, moduleConfig)
//
// Only supported with the optimizing compiler on 64-bit unix-like
// platforms (darwin, linux; amd64, arm64). Shared (threads) memories are
// not supported. Address space, not physical memory, is reserved: 8 GiB +
// 64 KiB per memory instance.
package guardpage
