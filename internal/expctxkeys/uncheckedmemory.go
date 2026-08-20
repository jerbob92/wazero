package expctxkeys

// UncheckedMemoryAccessKey is a context.Context Value key. Its value must be
// true to instruct wazevo's optimizing compiler to omit per-access memory
// bounds checks. See experimental.WithUncheckedMemoryAccess.
type UncheckedMemoryAccessKey struct{}
