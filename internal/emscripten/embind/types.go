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

func (bt *baseType) DestructorFunction(ctx context.Context, mod api.Module, pointer uint32) (*destructorFunc, error) {
	return nil, nil
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

func (bt *baseType) NativeType() api.ValueType {
	return api.ValueTypeI32
}

type registeredType interface {
	RawType() int32
	Name() string
	ArgPackAdvance() int32
	HasDestructorFunction() bool
	DestructorFunction(ctx context.Context, mod api.Module, pointer uint32) (*destructorFunc, error)
	FromWireType(ctx context.Context, mod api.Module, wt uint64) (any, error)
	ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error)
	ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error)
	HasDeleteObject() bool
	DeleteObject(ctx context.Context, mod api.Module, handle any) error
	NativeType() api.ValueType
}

type registerTypeOptions struct {
	ignoreDuplicateRegistrations bool
}

type awaitingDependency struct {
	cb func() error
}

type publicSymbol struct {
	argCount      *int32
	overloadTable map[int32]*publicSymbol
	fn            publicSymbolFn
	className     string
}

type registeredPointer struct {
	pointerType      *registeredPointerType
	constPointerType *registeredPointerType
}

type registeredTuple struct {
	name           string
	rawConstructor api.Function
	rawDestructor  api.Function
	elements       []*registeredTupleElement
}

type registeredTupleElement struct {
	getterReturnTypeID   int32
	getterPtr            int32
	getterSignaturePtr   int32
	getter               api.Function
	getterContext        int32
	setterArgumentTypeID int32
	setterPtr            int32
	setterSignaturePtr   int32
	setter               api.Function
	setterContext        int32
	read                 func(ctx context.Context, mod api.Module, ptr int32) (any, error)
	write                func(ctx context.Context, mod api.Module, ptr int32, o any) error
}

type registeredObject struct {
	name           string
	rawConstructor api.Function
	rawDestructor  api.Function
	fields         []*registeredObjectField
}

type registeredObjectField struct {
	fieldName          string
	getterReturnType   int32
	getter             api.Function
	getterContext      int32
	setterArgumentType int32
	setter             api.Function
	setterContext      int32
	read               func(ctx context.Context, mod api.Module, ptr int32) (any, error)
	write              func(ctx context.Context, mod api.Module, ptr int32, o any) error
}

type engine struct {
	mod                  api.Module
	publicSymbols        map[string]*publicSymbol
	registeredTypes      map[int32]registeredType
	typeDependencies     map[int32][]int32
	awaitingDependencies map[int32][]*awaitingDependency
	registeredConstants  map[string]*registeredConstant
	registeredEnums      map[string]*enumType
	registeredPointers   map[int32]*registeredPointer
	registeredClasses    map[string]*classType
	registeredTuples     map[int32]*registeredTuple
	registeredObjects    map[int32]*registeredObject
	registeredInstances  map[uint32]IEmvalClassBase
	emvalEngine          *emvalEngine
}

type undefinedType int8

var undefined = undefinedType(0)
