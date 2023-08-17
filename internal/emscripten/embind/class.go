package embind

import (
	"context"
	"github.com/tetratelabs/wazero/api"
)

type classType struct {
	baseType
	baseClass            *classType
	rawDestructor        api.Function
	getActualType        api.Function
	upcast               api.Function
	downcast             api.Function
	derivedClasses       []*classType
	goStruct             any
	hasGoStruct          bool
	pureVirtualFunctions []string
	methods              map[string]*publicSymbol
}

func (erc *classType) FromWireType(ctx context.Context, mod api.Module, value uint64) (any, error) {
	// @todo: implement me.
	return nil, nil
}

func (erc *classType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	// @todo: implement me.
	return 0, nil
}

func (erc *classType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	// @todo: implement me.
	return nil, nil
}

func (erc *classType) validate() error {
	// @todo: implement validator here.
	return nil
}

type EmvalClassBase struct {
}
