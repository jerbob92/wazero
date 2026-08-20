//go:build (darwin || linux) && (amd64 || arm64)

package guardpage

import "syscall"

// Supported is true when a guard-page reservation is available: a 64-bit
// unix-like platform where a large PROT_NONE reservation is cheap.
const Supported = true

// reserveSize is the virtual reservation for one guard-page backed 32-bit
// linear memory: the whole 4 GiB wasm address space, plus another 4 GiB so
// that any u32 base + u32 static offset + access size lands inside the
// reservation, plus a 64 KiB tail so the largest access starting at the
// very end still faults inside the region.
const reserveSize = 1<<33 + 1<<16

// reserve reserves size bytes of inaccessible (PROT_NONE) address space.
// Nothing is committed; the reservation only consumes virtual address
// space.
func reserve(size uintptr) ([]byte, error) {
	return syscall.Mmap(-1, 0, int(size), syscall.PROT_NONE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE)
}

// commit makes the first commitBytes of the reservation readable and
// writable.
func commit(region []byte, commitBytes uintptr) error {
	if commitBytes == 0 {
		return nil
	}
	return syscall.Mprotect(region[:commitBytes], syscall.PROT_READ|syscall.PROT_WRITE)
}

// release releases the whole reservation.
func release(region []byte) error {
	return syscall.Munmap(region)
}
