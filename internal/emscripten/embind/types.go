package embind

import (
	"context"
	"github.com/tetratelabs/wazero/api"
)

type publicSymbolFn func(ctx context.Context, mod api.Module, this any, arguments ...any) (any, error)

type registeredType struct {
	rawType              int32
	name                 string
	isVoid               bool
	destructorFunction   func(ctx context.Context, mod api.Module, pointer uint32) error
	fromWireType         func(ctx context.Context, mod api.Module, wt uint64) (any, error)
	toWireType           func(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error)
	argPackAdvance       int32
	readValueFromPointer func(ctx context.Context, mod api.Module, pointer uint32) (any, error)
}

type registerTypeOptions struct {
	ignoreDuplicateRegistrations bool
}

type awaitingDependency struct {
	cb func() error
}

type publicSymbol struct {
	argCount      int32
	overloadTable map[int32]*publicSymbol
	fn            publicSymbolFn
}

// @todo: implement classes.
type classType struct {
}

type engine struct {
	mod                  api.Module
	publicSymbols        map[string]*publicSymbol
	registeredTypes      map[int32]*registeredType
	typeDependencies     map[int32][]int32
	awaitingDependencies map[int32][]*awaitingDependency
}
