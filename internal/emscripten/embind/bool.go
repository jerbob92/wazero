package embind

import (
	"context"
	"fmt"
	"github.com/tetratelabs/wazero/api"
)

type boolType struct {
	baseType
	size     int32
	trueVal  int32
	falseVal int32
}

func (bt *boolType) FromWireType(ctx context.Context, mod api.Module, value uint64) (any, error) {
	// ambiguous emscripten ABI: sometimes return values are
	// true or false, and sometimes integers (0 or 1)
	return value > 0, nil
}

func (bt *boolType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	val, ok := o.(bool)
	if !ok {
		return 0, fmt.Errorf("value must be of type bool")
	}

	if val {
		return api.EncodeI32(bt.trueVal), nil
	}

	return api.EncodeI32(bt.falseVal), nil
}

func (bt *boolType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	if bt.size == 1 {
		val, _ := mod.Memory().ReadByte(pointer)
		return bt.FromWireType(ctx, mod, uint64(val))
	} else if bt.size == 2 {
		val, _ := mod.Memory().ReadUint16Le(pointer)
		return bt.FromWireType(ctx, mod, uint64(val))
	} else if bt.size == 4 {
		val, _ := mod.Memory().ReadUint32Le(pointer)
		return bt.FromWireType(ctx, mod, uint64(val))
	} else {
		return nil, fmt.Errorf("unknown boolean type size %d: %s", bt.size, bt.name)
	}
}
