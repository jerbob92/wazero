package guardpage

import (
	"os"
	"unsafe"

	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/experimental/guardsig"
)

// Allocator is a reference experimental.MemoryAllocator backed by a
// guard-page reservation. See the package doc for the full contract and how
// to pair it with experimental.WithUncheckedMemoryAccess.
var Allocator experimental.MemoryAllocator = experimental.MemoryAllocatorFunc(allocate)

func allocate(_, max uint64) experimental.LinearMemory {
	if !Supported || !guardsig.Supported() {
		// No reservation support, or no handler able to catch a fault
		// inside one: return a memory whose Reallocate always fails, per
		// LinearMemory's documented failure contract. The engine will
		// refuse to pair unchecked code with it (it does not implement
		// UnsafeLinearMemory).
		return brokenLinearMemory{}
	}

	region, err := reserve(reserveSize)
	if err != nil {
		return brokenLinearMemory{}
	}
	base := uintptr(unsafe.Pointer(&region[0]))
	if !guardsig.RegisterRegion(base, base+reserveSize) {
		_ = release(region)
		return brokenLinearMemory{}
	}
	return &linearMemory{region: region, max: max}
}

// linearMemory is a LinearMemory backed by a large PROT_NONE virtual
// reservation whose committed prefix grows via mprotect. The base address
// is stable for the lifetime of the memory, and any out-of-bounds access up
// to the guard window faults instead of reading/writing unrelated data,
// which is what allows the compiler to omit per-access bounds checks.
type linearMemory struct {
	region    []byte // the whole reservation
	max       uint64
	committed uintptr
}

var (
	_ experimental.LinearMemory       = (*linearMemory)(nil)
	_ experimental.UnsafeLinearMemory = (*linearMemory)(nil)
)

// IsUnsafeLinearMemory implements experimental.UnsafeLinearMemory.
func (l *linearMemory) IsUnsafeLinearMemory() bool { return true }

// Reallocate implements experimental.LinearMemory.
func (l *linearMemory) Reallocate(size uint64) []byte {
	if size > l.max || size > uint64(len(l.region)) {
		return nil
	}
	pageSize := uintptr(os.Getpagesize())
	aligned := (uintptr(size) + pageSize - 1) &^ (pageSize - 1)
	if aligned > l.committed {
		if err := commit(l.region, aligned); err != nil {
			return nil
		}
		l.committed = aligned
	}
	if size == 0 {
		// Keep a non-zero capacity so the returned slice's data pointer
		// (the reservation base) stays well-defined for unsafe.SliceData:
		// wazevo publishes it as the memory base even for an empty memory,
		// since checkless code dereferences base+addr directly.
		return l.region[:0:1]
	}
	return l.region[:size:size]
}

// Free implements experimental.LinearMemory.
func (l *linearMemory) Free() {
	if l.region == nil {
		return
	}
	guardsig.UnregisterRegion(uintptr(unsafe.Pointer(&l.region[0])))
	_ = release(l.region)
	l.region = nil
}

// brokenLinearMemory is returned when a reservation could not be made or
// registered, matching LinearMemory.Reallocate's documented "may return
// nil" failure contract rather than panicking inside the allocator itself.
// It does not implement UnsafeLinearMemory. In practice this almost always
// surfaces as an immediate instantiation failure (wazero indexes the result
// of the initial Reallocate(minBytes) call unconditionally) rather than a
// silent fallback to unprotected memory, which is the conservative behavior
// wanted here: if you explicitly asked for guard pages, failing loudly beats
// quietly running without them.
type brokenLinearMemory struct{}

func (brokenLinearMemory) Reallocate(uint64) []byte { return nil }
func (brokenLinearMemory) Free()                    {}
