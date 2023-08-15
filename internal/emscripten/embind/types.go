package embind

import (
	"context"
	"github.com/tetratelabs/wazero/api"
)

type publicSymbolFn func(ctx context.Context, this any, arguments ...any) (any, error)

type baseType struct {
	rawType        int32
	name           string
	argPackAdvance int32
}

func (bt *baseType) RawType() int32 {
	return bt.rawType
}

func (bt *baseType) Name() string {
	return bt.name
}

func (bt *baseType) ArgPackAdvance() int32 {
	return bt.argPackAdvance
}

func (bt *baseType) HasDestructorFunction() bool {
	return false
}

func (bt *baseType) DestructorFunction(ctx context.Context, mod api.Module, pointer uint32) error {
	return nil
}

func (bt *baseType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	return nil, nil
}

func (bt *baseType) HasDeleteObject() bool {
	return false
}

func (bt *baseType) DeleteObject(ctx context.Context, mod api.Module, handle any) error {
	return nil
}

type registeredType interface {
	RawType() int32
	Name() string
	ArgPackAdvance() int32
	HasDestructorFunction() bool
	DestructorFunction(ctx context.Context, mod api.Module, pointer uint32) error
	FromWireType(ctx context.Context, mod api.Module, wt uint64) (any, error)
	ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error)
	ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error)
	HasDeleteObject() bool
	DeleteObject(ctx context.Context, mod api.Module, handle any) error
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
	registeredTypes      map[int32]registeredType
	typeDependencies     map[int32][]int32
	awaitingDependencies map[int32][]*awaitingDependency
	registeredConstants  map[string]*registeredConstant
	registeredEnums      map[string]*enumType
	emvalEngine          *emvalEngine
}

type undefinedType int8

var undefined = undefinedType(0)
