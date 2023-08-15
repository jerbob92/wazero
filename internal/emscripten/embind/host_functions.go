package embind

import (
	"context"
	"errors"
	"fmt"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
	"log"
	"reflect"
	"strconv"
	"strings"
)

const FunctionEmbindRegisterFunction = "_embind_register_function"

var EmbindRegisterFunction = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterFunction,
	Name:       FunctionEmbindRegisterFunction,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"name", "argCount", "rawArgTypesAddr", "signature", "rawInvoker", "fn", "isAsync"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		namePtr := api.DecodeI32(stack[0])
		argCount := api.DecodeI32(stack[1])
		rawArgTypesAddr := api.DecodeI32(stack[2])
		signaturePtr := api.DecodeI32(stack[3])
		rawInvoker := api.DecodeI32(stack[4])
		fn := api.DecodeI32(stack[5])
		isAsync := api.DecodeI32(stack[6]) != 0

		argTypes, err := engine.heap32VectorToArray(argCount, rawArgTypesAddr)
		if err != nil {
			panic(fmt.Errorf("could not read arg types: %w", err))
		}

		name, err := engine.readCString(uint32(namePtr))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		// Create an api.Function to be able to invoke the function on the
		// Emscripten side.
		invokerFunc, err := engine.newInvokeFunc(signaturePtr, rawInvoker)
		if err != nil {
			panic(fmt.Errorf("could not create invoke func: %w", err))
		}

		// Set a default callback that errors out when not all types are resolved.
		err = engine.exposePublicSymbol(name, func(ctx context.Context, this any, arguments ...any) (any, error) {
			return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call %s due to unbound types", name), argTypes)
		}, argCount-1)
		if err != nil {
			panic(fmt.Errorf("could not expose public symbol: %w", err))
		}

		// When all types are resolved, replace the callback with the actual implementation.
		err = engine.whenDependentTypesAreResolved([]int32{}, argTypes, func(argTypes []registeredType) ([]registeredType, error) {
			invokerArgsArray := []registeredType{argTypes[0] /* return value */, nil /* no class 'this'*/}
			invokerArgsArray = append(invokerArgsArray, argTypes[1:]... /* actual params */)

			err = engine.replacePublicSymbol(name, engine.craftInvokerFunction(name, invokerArgsArray, nil /* no class 'this'*/, invokerFunc, fn, isAsync), argCount-1)
			if err != nil {
				return nil, err
			}

			return []registeredType{}, nil
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
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])
		name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		err = engine.registerType(rawType, &voidType{
			baseType: baseType{
				rawType:        rawType,
				name:           name,
				argPackAdvance: 0,
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
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])

		name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		err = engine.registerType(rawType, &boolType{
			baseType: baseType{
				rawType:        rawType,
				name:           name,
				argPackAdvance: 8,
			},
			size:     api.DecodeI32(stack[2]),
			trueVal:  api.DecodeI32(stack[3]),
			falseVal: api.DecodeI32(stack[4]),
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
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])
		name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		err = engine.registerType(rawType, &intType{
			baseType: baseType{
				rawType:        rawType,
				name:           name,
				argPackAdvance: 8,
			},
			size:   api.DecodeI32(stack[2]),
			signed: !strings.Contains(name, "unsigned"),
		}, nil)
	})},
}

const FunctionEmbindRegisterBigInt = "_embind_register_bigint"

var EmbindRegisterBigInt = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterBigInt,
	Name:       FunctionEmbindRegisterBigInt,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI64, wasm.ValueTypeI64},
	ParamNames: []string{"primitiveType", "name", "size", "minRange", "maxRange"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])
		name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		err = engine.registerType(rawType, &bigintType{
			baseType: baseType{
				rawType:        rawType,
				name:           name,
				argPackAdvance: 8,
			},
			size:   api.DecodeI32(stack[2]),
			signed: !strings.HasPrefix(name, "u"),
		}, nil)
	})},
}

const FunctionEmbindRegisterFloat = "_embind_register_float"

var EmbindRegisterFloat = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterFloat,
	Name:       FunctionEmbindRegisterFloat,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "name", "size"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])
		name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		err = engine.registerType(rawType, &floatType{
			baseType: baseType{
				rawType:        rawType,
				name:           name,
				argPackAdvance: 8,
			},
			size: api.DecodeI32(stack[2]),
		}, nil)
	})},
}

const FunctionEmbindRegisterStdString = "_embind_register_std_string"

var EmbindRegisterStdString = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterStdString,
	Name:       FunctionEmbindRegisterStdString,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "name"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])
		name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		err = engine.registerType(rawType, &stdStringType{
			baseType: baseType{
				rawType:        rawType,
				name:           name,
				argPackAdvance: 8,
			},
			stdStringIsUTF8: name == "std::string",
		}, nil)
	})},
}

const FunctionEmbindRegisterStdWString = "_embind_register_std_wstring"

var EmbindRegisterStdWString = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterStdWString,
	Name:       FunctionEmbindRegisterStdWString,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "charSize", "name"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])
		name, err := engine.readCString(uint32(api.DecodeI32(stack[2])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		err = engine.registerType(rawType, &stdWStringType{
			baseType: baseType{
				rawType:        rawType,
				name:           name,
				argPackAdvance: 8,
			},
			charSize: api.DecodeI32(stack[1]),
		}, nil)
	})},
}

const FunctionEmbindRegisterEmval = "_embind_register_emval"

var EmbindRegisterEmval = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterEmval,
	Name:       FunctionEmbindRegisterEmval,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "name"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])
		name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		err = engine.registerType(rawType, &emvalType{
			baseType: baseType{
				rawType:        rawType,
				name:           name,
				argPackAdvance: 8,
			},
			engine: engine,
		}, &registerTypeOptions{
			ignoreDuplicateRegistrations: true,
		})
	})},
}

const FunctionEmbindRegisterMemoryView = "_embind_register_memory_view"

var EmbindRegisterMemoryView = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterMemoryView,
	Name:       FunctionEmbindRegisterMemoryView,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "dataTypeIndex", "name"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])
		dataTypeIndex := api.DecodeI32(stack[1])
		name, err := engine.readCString(uint32(api.DecodeI32(stack[2])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		typeMapping := []any{
			int8(0),
			uint8(0),
			int16(0),
			uint16(0),
			int32(0),
			uint32(0),
			float32(0),
			float64(0),
			int64(0),
			uint64(0),
		}

		if dataTypeIndex < 0 || int(dataTypeIndex) >= len(typeMapping) {
			panic(fmt.Errorf("invalid memory view data type index: %d", dataTypeIndex))
		}

		sizeMapping := []uint32{
			1, // int8
			1, // uint8
			2, // int16
			2, // uint16
			4, // int32
			4, // uint32
			4, // float32
			8, // float64
			8, // int64
			8, // uint64
		}

		err = engine.registerType(rawType, &memoryViewType{
			baseType: baseType{
				rawType:        rawType,
				name:           name,
				argPackAdvance: 8,
			},
			dataTypeIndex: dataTypeIndex,
			nativeSize:    sizeMapping[dataTypeIndex],
			nativeType:    typeMapping[dataTypeIndex],
		}, &registerTypeOptions{
			ignoreDuplicateRegistrations: true,
		})
	})},
}

const FunctionEmbindRegisterConstant = "_embind_register_constant"

var EmbindRegisterConstant = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterConstant,
	Name:       FunctionEmbindRegisterConstant,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeF64},
	ParamNames: []string{"name", "type", "value"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		name, err := engine.readCString(uint32(api.DecodeI32(stack[0])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		rawType := api.DecodeI32(stack[1])

		// @todo: this seems to work properly except for float and bool.
		// JS VM does auto conversion, so we don't get much info from the
		// Emscripten implementation. If I just pass the stack here, none of
		// the values are correct except for double.
		constantValue := uint64(api.DecodeF64(stack[2]))

		err = engine.whenDependentTypesAreResolved([]int32{}, []int32{rawType}, func(argTypes []registeredType) ([]registeredType, error) {
			registeredType := argTypes[0]
			val, err := registeredType.FromWireType(ctx, engine.mod, constantValue)
			if err != nil {
				return nil, fmt.Errorf("could not initialize constant %s: %w", name, err)
			}

			_, ok := engine.registeredConstants[name]
			if !ok {
				engine.registeredConstants[name] = &registeredConstant{
					name: name,
				}
			}

			engine.registeredConstants[name].hasCppValue = true
			engine.registeredConstants[name].cppValue = val
			engine.registeredConstants[name].rawCppValue = constantValue

			return nil, engine.registeredConstants[name].validate()
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
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])
		name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		_, ok := engine.registeredEnums[name]
		if !ok {
			engine.registeredEnums[name] = &enumType{
				valuesByName:     map[string]*enumValue{},
				valuesByCppValue: map[any]*enumValue{},
				valuesByGoValue:  map[any]*enumValue{},
			}
		}

		engine.registeredEnums[name].baseType = baseType{
			rawType:        rawType,
			name:           name,
			argPackAdvance: 8,
		}

		engine.registeredEnums[name].intHelper = intType{
			size:   api.DecodeI32(stack[2]),
			signed: api.DecodeI32(stack[3]) > 0,
		}

		err = engine.registerType(rawType, engine.registeredEnums[name], nil)
	})},
}

const FunctionEmbindRegisterEnumValue = "_embind_register_enum_value"

var EmbindRegisterEnumValue = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterEnumValue,
	Name:       FunctionEmbindRegisterEnumValue,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawEnumType", "name", "enumValue"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])
		name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		registeredType, ok := engine.registeredTypes[rawType]
		if !ok {
			typeName, err := engine.getTypeName(ctx, rawType)
			if err != nil {
				panic(err)
			}
			panic(fmt.Errorf("%s has unknown type %s", name, typeName))
		}

		enumType := registeredType.(*enumType)
		enumWireValue, err := enumType.intHelper.FromWireType(ctx, mod, stack[2])
		if err != nil {
			panic(fmt.Errorf("could not read value for enum %s", name))
		}

		_, ok = enumType.valuesByName[name]
		if !ok {
			enumType.valuesByName[name] = &enumValue{
				name: name,
			}
		}

		if enumType.valuesByName[name].hasCppValue {
			panic(fmt.Errorf("enum value %s for enum %s was already registered", name, enumType.name))
		}

		enumType.valuesByName[name].hasCppValue = true
		enumType.valuesByName[name].cppValue = enumWireValue
		enumType.valuesByCppValue[enumWireValue] = enumType.valuesByName[name]
	})},
}

const FunctionEmvalTakeValue = "_emval_take_value"

var EmvalTakeValue = &wasm.HostFunc{
	ExportName:  FunctionEmvalTakeValue,
	Name:        FunctionEmvalTakeValue,
	ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"type", "arg"},
	ResultTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"emval_handle"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		rawType := api.DecodeI32(stack[0])

		registeredType, ok := engine.registeredTypes[rawType]
		if !ok {
			typeName, err := engine.getTypeName(ctx, rawType)
			if err != nil {
				panic(err)
			}
			panic(fmt.Errorf("_emval_take_value has unknown type %s", typeName))
		}

		arg := api.DecodeI32(stack[1])
		value, err := registeredType.ReadValueFromPointer(ctx, mod, uint32(arg))
		if err != nil {
			panic(fmt.Errorf("could not take value for _emval_take_value: %w", err))
		}

		id := engine.emvalEngine.toHandle(value)
		stack[0] = api.EncodeI32(id)
	})},
}

const FunctionEmvalIncref = "_emval_incref"

var EmvalIncref = &wasm.HostFunc{
	ExportName: FunctionEmvalIncref,
	Name:       FunctionEmvalIncref,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames: []string{"handle"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		handle := api.DecodeI32(stack[0])
		err := engine.emvalEngine.allocator.incref(handle)
		if err != nil {
			panic(fmt.Errorf("could not emval incref: %w", err))
		}
	})},
}

const FunctionEmvalDecref = "_emval_decref"

var EmvalDecref = &wasm.HostFunc{
	ExportName: FunctionEmvalDecref,
	Name:       FunctionEmvalDecref,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames: []string{"handle"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		handle := api.DecodeI32(stack[0])
		err := engine.emvalEngine.allocator.decref(handle)
		if err != nil {
			panic(fmt.Errorf("could not emval incref: %w", err))
		}
	})},
}

const FunctionEmvalRegisterSymbol = "_emval_register_symbol"

var EmvalRegisterSymbol = &wasm.HostFunc{
	ExportName: FunctionEmvalRegisterSymbol,
	Name:       FunctionEmvalRegisterSymbol,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames: []string{"address"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		address := uint32(api.DecodeI32(stack[0]))
		name, err := engine.readCString(address)
		if err != nil {
			panic(fmt.Errorf("could not get symbol name"))
		}
		engine.emvalEngine.symbols[address] = name
	})},
}

const FunctionEmvalGetGlobal = "_emval_get_global"

var EmvalGetGlobal = &wasm.HostFunc{
	ExportName:  FunctionEmvalGetGlobal,
	Name:        FunctionEmvalGetGlobal,
	ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"name"},
	ResultTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"handle"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		name := api.DecodeI32(stack[0])

		if name == 0 {
			stack[0] = api.EncodeI32(engine.emvalEngine.toHandle(engine.emvalEngine.getGlobal("")))
		} else {
			name, err := engine.getStringOrSymbol(uint32(name))
			if err != nil {
				panic(fmt.Errorf("could not get symbol name"))
			}
			stack[0] = api.EncodeI32(engine.emvalEngine.toHandle(engine.emvalEngine.getGlobal(name)))
		}
	})},
}

const FunctionEmvalAs = "_emval_as"

var EmvalAs = &wasm.HostFunc{
	ExportName:  FunctionEmvalAs,
	Name:        FunctionEmvalAs,
	ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"handle", "returnType", "destructorsRef"},
	ResultTypes: []wasm.ValueType{wasm.ValueTypeF64},
	ResultNames: []string{"val"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		id := api.DecodeI32(stack[0])
		handle, err := engine.emvalEngine.toValue(id)
		if err != nil {
			panic(fmt.Errorf("could not find handle: %w", err))
		}

		returnType, err := engine.requireRegisteredType(ctx, api.DecodeI32(stack[1]), "emval::as")
		if err != nil {
			panic(fmt.Errorf("could not require registered type: %w", err))
		}

		var destructors = &[]*destructorFunc{}
		rd := engine.emvalEngine.toHandle(destructors)
		ok := mod.Memory().WriteUint32Le(uint32(api.DecodeI32(stack[2])), uint32(rd))
		if !ok {
			panic(fmt.Errorf("could not write destructor ref to memory"))
		}

		returnVal, err := returnType.ToWireType(ctx, mod, destructors, handle)
		if err != nil {
			panic(fmt.Errorf("could not call toWireType on _emval_as: %w", err))
		}

		stack[0] = returnVal
	})},
}

const FunctionEmvalNew = "_emval_new"

var EmvalNew = &wasm.HostFunc{
	ExportName:  FunctionEmvalNew,
	Name:        FunctionEmvalNew,
	ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"handle", "argCount", "argTypes", "args"},
	ResultTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"val"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		id := api.DecodeI32(stack[0])

		handle, err := engine.emvalEngine.toValue(id)
		if err != nil {
			panic(fmt.Errorf("could not get value of handle: %w", err))
		}

		argCount := int(api.DecodeI32(stack[1]))
		argsTypeBase := uint32(api.DecodeI32(stack[2]))
		argsBase := uint32(api.DecodeI32(stack[3]))

		args := make([]any, argCount)
		argTypeNames := make([]string, argCount)
		for i := 0; i < argCount; i++ {
			argType, ok := mod.Memory().ReadUint32Le(argsTypeBase + (4 * uint32(i)))
			if !ok {
				panic(fmt.Errorf("could not read arg type for arg %d from memory", i))
			}

			registeredArgType, err := engine.requireRegisteredType(ctx, int32(argType), fmt.Sprintf("argument %d", i))
			if err != nil {
				panic(fmt.Errorf("could not require registered type: %w", err))
			}

			args[i], err = registeredArgType.ReadValueFromPointer(ctx, mod, argsBase+(8*uint32(i)))
			if err != nil {
				panic(fmt.Errorf("could not read arg value from pointer for arg %d, %w", i, err))
			}

			argTypeNames[i] = registeredArgType.Name()
		}

		var res any
		c, ok := handle.(EmvalConstructor)
		if ok {
			res, err = c.New(argTypeNames, args...)
			if err != nil {
				panic(fmt.Errorf("could not instaniate new value on %T with New(): %w", handle, err))
			}
		} else {
			typeElem := reflect.TypeOf(handle)

			// If we received a pointer, resolve the struct behind it.
			if typeElem.Kind() == reflect.Pointer {
				typeElem = typeElem.Elem()
			}

			// Make new instance of struct.
			newElem := reflect.New(typeElem)

			// Set the values on the struct if we need to/can.
			if argCount > 0 {
				if typeElem.Kind() != reflect.Struct {
					panic(fmt.Errorf("could not instaniate new value of %T: arguments required but can only be set on a struct", handle))
				}

				for i := 0; i < argCount; i++ {
					for fieldI := 0; fieldI < typeElem.NumField(); fieldI++ {
						var err error
						func() {
							defer func() {
								if recoverErr := recover(); recoverErr != nil {
									realError, ok := recoverErr.(error)
									log.Println(realError)
									if ok {
										err = fmt.Errorf("could not set arg %d with embind_arg tag on emval %T: %w", i, handle, realError)
									}
									err = fmt.Errorf("could not set arg %d with embind_arg tag on emval %T: %v", i, handle, recoverErr)
								}
							}()

							val := typeElem.Field(fieldI)
							if val.Tag.Get("embind_arg") == strconv.Itoa(i) {
								f := newElem.Elem().FieldByName(val.Name)
								if f.IsValid() && f.CanSet() {
									f.Set(reflect.ValueOf(args[i]))
								}
							}
						}()
						if err != nil {
							panic(fmt.Errorf("could not instaniate new value of %T: %w", handle, err))
						}
					}
				}
			}

			if reflect.TypeOf(handle).Kind() == reflect.Pointer {
				res = newElem.Interface()
			} else {
				res = newElem.Elem().Interface()
			}
		}

		stack[0] = api.EncodeI32(engine.emvalEngine.toHandle(res))
	})},
}

const FunctionEmvalSetProperty = "_emval_set_property"

var EmvalSetProperty = &wasm.HostFunc{
	ExportName: FunctionEmvalSetProperty,
	Name:       FunctionEmvalSetProperty,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"handle", "key", "value"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		handle, err := engine.emvalEngine.toValue(api.DecodeI32(stack[0]))
		if err != nil {
			panic(fmt.Errorf("could not find handle: %w", err))
		}

		key, err := engine.emvalEngine.toValue(api.DecodeI32(stack[1]))
		if err != nil {
			panic(fmt.Errorf("could not find key: %w", err))
		}

		val, err := engine.emvalEngine.toValue(api.DecodeI32(stack[2]))
		if err != nil {
			panic(fmt.Errorf("could not find val: %w", err))
		}

		keyString, ok := key.(string)
		if !ok {
			panic(fmt.Errorf("could not set property on emval %T: %w", handle, errors.New("key is not of type string")))
		}

		f, err := engine.emvalEngine.getElemField(handle, keyString)
		if err != nil {
			panic(fmt.Errorf("could not set property %s on emval %T: %w", keyString, handle, err))
		}

		defer func() {
			if err := recover(); err != nil {
				realError, ok := err.(error)
				if ok {
					panic(fmt.Errorf("could not set property %s on emval %T: %w", keyString, handle, realError))
				}
				panic(fmt.Errorf("could not set property %s on emval %T: %v", keyString, handle, err))
			}
		}()

		f.Set(reflect.ValueOf(val))
	})},
}

const FunctionEmvalGetProperty = "_emval_get_property"

var EmvalGetProperty = &wasm.HostFunc{
	ExportName:  FunctionEmvalGetProperty,
	Name:        FunctionEmvalGetProperty,
	ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"handle", "key"},
	ResultTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"value"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		handle, err := engine.emvalEngine.toValue(api.DecodeI32(stack[0]))
		if err != nil {
			panic(fmt.Errorf("could not find handle: %w", err))
		}

		key, err := engine.emvalEngine.toValue(api.DecodeI32(stack[1]))
		if err != nil {
			panic(fmt.Errorf("could not find key: %w", err))
		}

		keyString, ok := key.(string)
		if !ok {
			panic(fmt.Errorf("could not get property on emval %T: %w", handle, errors.New("key is not of type string")))
		}

		f, err := engine.emvalEngine.getElemField(handle, keyString)
		if err != nil {
			panic(fmt.Errorf("could not get property %s on emval %T: %w", keyString, handle, err))
		}

		defer func() {
			if err := recover(); err != nil {
				realError, ok := err.(error)
				if ok {
					panic(fmt.Errorf("could not get property %s on emval %T: %w", keyString, handle, realError))
				}
				panic(fmt.Errorf("could not get property %s on emval %T: %v", keyString, handle, err))
			}
		}()

		stack[0] = api.EncodeI32(engine.emvalEngine.toHandle(f.Interface()))
	})},
}

const FunctionEmvalNewCString = "_emval_new_cstring"

var EmvalNewCString = &wasm.HostFunc{
	ExportName:  FunctionEmvalNewCString,
	Name:        FunctionEmvalNewCString,
	ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"v"},
	ResultTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"handle"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		v := api.DecodeI32(stack[0])
		name, err := engine.getStringOrSymbol(uint32(v))
		if err != nil {
			panic(fmt.Errorf("could not get symbol name"))
		}
		stack[0] = api.EncodeI32(engine.emvalEngine.toHandle(name))
	})},
}

const FunctionEmvalRunDestructors = "_emval_run_destructors"

var EmvalRunDestructors = &wasm.HostFunc{
	ExportName: FunctionEmvalRunDestructors,
	Name:       FunctionEmvalRunDestructors,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames: []string{"handle"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		id := api.DecodeI32(stack[0])
		destructorsVal, err := engine.emvalEngine.toValue(id)
		if err != nil {
			panic(fmt.Errorf("could not find handle: %w", err))
		}

		destructors := destructorsVal.(*[]*destructorFunc)

		err = engine.runDestructors(ctx, *destructors)
		if err != nil {
			panic(fmt.Errorf("could not run destructors: %w", err))
		}

		err = engine.emvalEngine.allocator.decref(id)
		if err != nil {
			panic(fmt.Errorf("could not run decref id %d: %w", id, err))
		}
	})},
}

const FunctionEmvalGetMethodCaller = "_emval_get_method_caller"

var EmvalGetMethodCaller = &wasm.HostFunc{
	ExportName:  FunctionEmvalGetMethodCaller,
	Name:        FunctionEmvalGetMethodCaller,
	ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"argCount, argTypes"},
	ResultTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"id"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		argCount := int(api.DecodeI32(stack[0]))
		argsTypeBase := uint32(api.DecodeI32(stack[1]))

		typeNames := make([]string, argCount)
		argTypes := make([]registeredType, argCount)
		for i := 0; i < argCount; i++ {
			argType, ok := mod.Memory().ReadUint32Le(argsTypeBase + (4 * uint32(i)))
			if !ok {
				panic(fmt.Errorf("could not read arg type for arg %d from memory", i))
			}

			registeredType, err := engine.requireRegisteredType(ctx, int32(argType), fmt.Sprintf("argument %d", i))
			if err != nil {
				panic(fmt.Errorf("could not require registered type: %w", err))
			}

			typeNames[i] = registeredType.Name()
			argTypes[i] = registeredType
		}

		signatureName := typeNames[0] + "_$" + strings.Join(typeNames[1:], "_") + "$"

		id, ok := engine.emvalEngine.registeredMethodIds[signatureName]
		if ok {
			stack[0] = api.EncodeI32(id)
			return
		}

		newID := engine.emvalEngine.registeredMethodCount
		newRegisteredMethod := &emvalRegisteredMethod{
			id:       newID,
			argTypes: argTypes,
			name:     signatureName,
		}
		engine.emvalEngine.registeredMethodIds[signatureName] = newID
		engine.emvalEngine.registeredMethods[newID] = newRegisteredMethod
		engine.emvalEngine.registeredMethodCount++

		stack[0] = api.EncodeI32(newID)
		return
	})},
}

const FunctionEmvalCallMethod = "_emval_call_method"

var EmvalCallMethod = &wasm.HostFunc{
	ExportName:  FunctionEmvalCallMethod,
	Name:        FunctionEmvalCallMethod,
	ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"caller", "id", "methodName", "destructorsRef", "args"},
	ResultTypes: []wasm.ValueType{wasm.ValueTypeF64},
	ResultNames: []string{"value"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		caller := api.DecodeI32(stack[0])

		registeredMethod, ok := engine.emvalEngine.registeredMethods[caller]
		if !ok {
			panic(fmt.Errorf("could not call method with ID %d", caller))
		}

		id := api.DecodeI32(stack[1])
		handle, err := engine.emvalEngine.toValue(id)
		if err != nil {
			panic(fmt.Errorf("could not find handle: %w", err))
		}

		methodName, err := engine.getStringOrSymbol(uint32(api.DecodeI32(stack[2])))
		if err != nil {
			panic(fmt.Errorf("could not get symbol name"))
		}

		argsBase := uint32(api.DecodeI32(stack[4]))
		destructorsRef := uint32(api.DecodeI32(stack[3]))

		res, err := engine.emvalEngine.callMethod(ctx, mod, registeredMethod, handle, methodName, destructorsRef, argsBase)
		if err != nil {
			panic(fmt.Errorf("could not call %s on %T: %w", methodName, handle, err))
		}
		stack[0] = api.EncodeF64(float64(res))
	})},
}

const FunctionEmvalCallVoidMethod = "_emval_call_void_method"

var EmvalCallVoidMethod = &wasm.HostFunc{
	ExportName: FunctionEmvalCallVoidMethod,
	Name:       FunctionEmvalCallVoidMethod,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"caller", "handle", "methodName", "args"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		caller := api.DecodeI32(stack[0])

		registeredMethod, ok := engine.emvalEngine.registeredMethods[caller]
		if !ok {
			panic(fmt.Errorf("could not call method with ID %d", caller))
		}

		id := api.DecodeI32(stack[1])
		handle, err := engine.emvalEngine.toValue(id)
		if err != nil {
			panic(fmt.Errorf("could not find handle: %w", err))
		}

		methodName, err := engine.getStringOrSymbol(uint32(api.DecodeI32(stack[2])))
		if err != nil {
			panic(fmt.Errorf("could not get symbol name"))
		}

		argsBase := uint32(api.DecodeI32(stack[3]))

		_, err = engine.emvalEngine.callMethod(ctx, mod, registeredMethod, handle, methodName, 0, argsBase)
		if err != nil {
			panic(fmt.Errorf("could not call %s on %T: %w", methodName, handle, err))
		}
	})},
}
