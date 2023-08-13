package embind

import (
	"context"
	"fmt"
	"log"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

const FunctionEmbindRegisterFunction = "_embind_register_function"

var EmbindRegisterFunction = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterFunction,
	Name:       FunctionEmbindRegisterFunction,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"name", "argCount", "rawArgTypesAddr", "signature", "rawInvoker", "fn", "isAsync"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*embindEngine)

		argTypes, err := engine.heap32VectorToArray(api.DecodeI32(stack[1]), api.DecodeI32(stack[2]))
		if err != nil {
			panic(fmt.Errorf("could not read arg types: %w", err))
		}

		name, err := engine.readCString(uint32(api.DecodeI32(stack[0])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		// Create an api.Function to be able to invoke the function on the
		// Emscripten side.
		rawInvoker := engine.embind__requireFunction(api.DecodeI32(stack[3]), api.DecodeI32(stack[4]))

		// Set a default callback that errors out when not all types are resolved.
		engine.exposePublicSymbol(name, func(ctx context.Context, mod api.Module, this any, arguments ...any) (any, error) {
			return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call %s due to unbound types", name), argTypes)
		}, api.DecodeI32(stack[1])-1)

		// When all types are resolved, replace the callback with the actual implementation.
		err = engine.whenDependentTypesAreResolved([]int32{}, argTypes, func(argTypes []*registeredType) ([]*registeredType, error) {
			invokerArgsArray := []*registeredType{argTypes[0] /* return value */, nil /* no class 'this'*/}
			invokerArgsArray = append(invokerArgsArray, argTypes[1:]... /* actual params */)

			err = engine.replacePublicSymbol(name, engine.craftInvokerFunction(name, invokerArgsArray, nil /* no class 'this'*/, rawInvoker, api.DecodeI32(stack[5]), api.DecodeI32(stack[6]) != 0), api.DecodeI32(stack[1])-1)
			if err != nil {
				return nil, err
			}

			return []*registeredType{}, nil
		})
		if err != nil {
			panic(fmt.Errorf("could not setup type dependenant lookup callbacks: %w", err))
		}
	})},
}

const FunctionEmbindRegisterVoid = "_embind_register_void"

var EmbindRegisterVoid = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterVoid,
	Name:       FunctionEmbindRegisterVoid,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "name"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*embindEngine)

		rawType := api.DecodeI32(stack[0])
		name, err := engine.readCString(uint32(api.DecodeI32(stack[0])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		err = engine.registerType(rawType, &registeredType{
			isVoid:         true,
			rawType:        rawType,
			name:           name,
			argPackAdvance: 0,
			fromWireType: func(ctx context.Context, mod api.Module, wt uint64) (any, error) {
				return nil, nil
			},
			toWireType: func(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
				// TODO: assert if anything else is given?
				return 0, nil
			},
		}, nil)

		if err != nil {
			panic(fmt.Errorf("could not register: %w", err))
		}
	})},
}

const FunctionEmbindRegisterBool = "_embind_register_bool"

var EmbindRegisterBool = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterBool,
	Name:       FunctionEmbindRegisterBool,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "name", "size", "trueValue", "falseValue"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*embindEngine)

		rawType := api.DecodeI32(stack[0])

		log.Printf("boolean: %d", rawType)

		name, err := engine.readCString(uint32(api.DecodeI32(stack[0])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		size := api.DecodeI32(stack[2])
		//shift := getShiftFromSize(size)
		trueVal := api.DecodeI32(stack[3])
		falseVal := api.DecodeI32(stack[4])

		fromWireType := func(ctx context.Context, mod api.Module, wt uint64) (any, error) {
			// ambiguous emscripten ABI: sometimes return values are
			// true or false, and sometimes integers (0 or 1)
			return wt > 0, nil
		}

		err = engine.registerType(rawType, &registeredType{
			rawType:      rawType,
			name:         name,
			fromWireType: fromWireType,
			toWireType: func(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
				val, ok := o.(bool)
				if !ok {
					return 0, fmt.Errorf("value must be of type bool")
				}

				if val {
					return api.EncodeI32(trueVal), nil
				}

				return api.EncodeI32(falseVal), nil
			},
			argPackAdvance: 8,
			readValueFromPointer: func(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
				if size == 1 {
					val, _ := mod.Memory().ReadByte(pointer)
					return fromWireType(ctx, mod, uint64(val))
				} else if size == 2 {
					val, _ := mod.Memory().ReadUint16Le(pointer)
					return fromWireType(ctx, mod, uint64(val))
				} else if size == 4 {
					val, _ := mod.Memory().ReadUint32Le(pointer)
					return fromWireType(ctx, mod, uint64(val))
				} else {
					return nil, fmt.Errorf("unknown boolean type size %d: %s", size, name)
				}
			},
		}, nil)
	})},
}

const FunctionEmbindRegisterInteger = "_embind_register_integer"

var EmbindRegisterInteger = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterInteger,
	Name:       FunctionEmbindRegisterInteger,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "name", "size", "minRange", "maxRange"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		//engine := MustGetEngineFromContext(ctx, mod).(*embindEngine)

		rawType := api.DecodeI32(stack[0])
		log.Printf("Integer: %d", rawType)

		/*
			rawType := api.DecodeI32(stack[0])
			name, err := engine.readCString( uint32(api.DecodeI32(stack[0])))
			if err != nil {
				panic(fmt.Errorf("could not read name: %w", err))
			}

			size := api.DecodeI32(stack[2])
			shift := getShiftFromSize(size)
			minRange := api.DecodeI32(stack[3])
			maxRange := int64(api.DecodeI32(stack[4]))
			// LLVM doesn't have signed and unsigned 32-bit types, so u32 literals come
			// out as 'i32 -1'. Always treat those as max u32.
			if maxRange == -1 {
				maxRange = 4294967295
			}

			log.Printf("register integer %s: %d %d %d %d %d", name, rawType, size, shift, minRange, maxRange)
		*/
		// @todo: implement me.
	})},
}

const FunctionEmbindRegisterBigInt = "_embind_register_bigint"

var EmbindRegisterBigInt = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterBigInt,
	Name:       FunctionEmbindRegisterBigInt,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI64, wasm.ValueTypeI64},
	ParamNames: []string{"primitiveType", "name", "size", "minRange", "maxRange"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		//engine := MustGetEngineFromContext(ctx, mod).(*embindEngine)

		rawType := api.DecodeI32(stack[0])
		log.Printf("Bigint: %d", rawType)

		/*
			rawType := api.DecodeI32(stack[0])
			name, err := engine.readCString( uint32(api.DecodeI32(stack[0])))
			if err != nil {
				panic(fmt.Errorf("could not read name: %w", err))
			}

			size := api.DecodeI32(stack[2])
			shift := getShiftFromSize(size)
			minRange := api.DecodeI32(stack[3])
			maxRange := int64(api.DecodeI32(stack[4]))
		*/

		//log.Printf("register bigint %s: %d %d %d %d %d", name, rawType, size, shift, minRange, maxRange)
		// @todo: generate code that contains bigints.
		// @todo: implement me.
	})},
}

const FunctionEmbindRegisterFloat = "_embind_register_float"

var EmbindRegisterFloat = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterFloat,
	Name:       FunctionEmbindRegisterFloat,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "name", "size"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*embindEngine)

		rawType := api.DecodeI32(stack[0])
		log.Printf("Float: %d", rawType)
		name, err := engine.readCString(uint32(api.DecodeI32(stack[0])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		size := api.DecodeI32(stack[2])
		shift, err := engine.getShiftFromSize(size)
		if err != nil {
			panic(fmt.Errorf("could not get shift size: %w", err))
		}

		err = engine.registerType(rawType, &registeredType{
			rawType: rawType,
			name:    name,
			fromWireType: func(ctx context.Context, mod api.Module, value uint64) (any, error) {
				if size == 4 {
					return api.DecodeF32(value), nil
				}
				if size == 8 {
					return api.DecodeF64(value), nil
				}
				return nil, fmt.Errorf("unknown float size")
			},
			toWireType: func(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
				if size == 4 {
					f32Val, ok := o.(float32)
					if ok {
						return api.EncodeF32(f32Val), nil
					}

					return 0, fmt.Errorf("value must be of type float32")
				}

				if size == 8 {
					f64Val, ok := o.(float64)
					if ok {
						return api.EncodeF64(f64Val), nil
					}

					return 0, fmt.Errorf("value must be of type float64")
				}

				return 0, fmt.Errorf("unknown float size")
			},
			argPackAdvance: 8,
			readValueFromPointer: func(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
				if shift == 2 {
					val, _ := mod.Memory().ReadFloat32Le(pointer)
					return val, nil
				} else if shift == 3 {
					val, _ := mod.Memory().ReadUint64Le(pointer)
					return val, nil
				}

				return nil, fmt.Errorf("unknown float type: %s", name)
			},
		}, nil)
	})},
}

const FunctionEmbindRegisterStdString = "_embind_register_std_string"

var EmbindRegisterStdString = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterStdString,
	Name:       FunctionEmbindRegisterStdString,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "name"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(context.Context, api.Module, []uint64) {
		//log.Println("register std_string")
		// @todo: implement me.
	})},
}

const FunctionEmbindRegisterStdWString = "_embind_register_std_wstring"

var EmbindRegisterStdWString = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterStdWString,
	Name:       FunctionEmbindRegisterStdWString,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "charSize", "name"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		//log.Println("register std_wstring")
		// @todo: implement me.
	})},
}

const FunctionEmbindRegisterEmval = "_embind_register_emval"

var EmbindRegisterEmval = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterEmval,
	Name:       FunctionEmbindRegisterEmval,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "name"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		//log.Println("register emval")
		// @todo: implement me.
		rawType := api.DecodeI32(stack[0])
		log.Printf("emval: %d", rawType)
	})},
}

const FunctionEmbindRegisterMemoryView = "_embind_register_memory_view"

var EmbindRegisterMemoryView = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterMemoryView,
	Name:       FunctionEmbindRegisterMemoryView,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "dataTypeIndex", "name"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		//log.Println("register memory_view")
		// @todo: implement me.
		rawType := api.DecodeI32(stack[0])
		log.Printf("memory view: %d", rawType)
	})},
}

const FunctionEmbindRegisterConstant = "_embind_register_constant"

var registeredConstants = map[string]any{}

var EmbindRegisterConstant = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterConstant,
	Name:       FunctionEmbindRegisterConstant,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeF64},
	ParamNames: []string{"name", "type", "value"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*embindEngine)

		name, err := engine.readCString(uint32(api.DecodeI32(stack[0])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}
		rawType := api.DecodeI32(stack[1])
		log.Printf("Constant needs type %d", rawType)

		err = engine.whenDependentTypesAreResolved([]int32{}, []int32{rawType}, func(argTypes []*registeredType) ([]*registeredType, error) {
			log.Printf("Constant has type %d", rawType)

			registeredType := argTypes[0]
			log.Println(stack[2])
			val, err := registeredType.fromWireType(ctx, engine.mod, stack[2])
			if err != nil {
				return nil, fmt.Errorf("could not initialize constant %s: %w", name, err)
			}
			registeredConstants[name] = val
			log.Println(registeredConstants)
			return nil, nil
		})

		if err != nil {
			panic(err)
		}
	})},
}

const FunctionEmbindRegisterEnum = "_embind_register_enum"

var EmbindRegisterEnum = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterEnum,
	Name:       FunctionEmbindRegisterEnum,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "name", "size", "isSigned"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*embindEngine)

		rawType := api.DecodeI32(stack[0])
		//size := api.DecodeI32(stack[2])
		//shift := getShiftFromSize(size)
		name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		err = engine.registerType(rawType, &registeredType{
			rawType: rawType,
			name:    name,
			fromWireType: func(ctx context.Context, mod api.Module, value uint64) (any, error) {
				// @todo: implement me.
				return nil, nil
			},
			toWireType: func(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
				// @todo: implement me.
				return 0, nil
			},
			argPackAdvance: 8,
			// @todo: implement readValueFromPointer
		}, nil)

		engine.exposePublicSymbol(name, func(ctx context.Context, mod api.Module, this any, arguments ...any) (any, error) {
			return nil, nil
		}, 0)
	})},
}

const FunctionEmbindRegisterEnumValue = "_embind_register_enum_value"

var EmbindRegisterEnumValue = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterEnumValue,
	Name:       FunctionEmbindRegisterEnumValue,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawEnumType", "name", "enumValue"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {

	})},
}
