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

		publicSymbolArgs := argCount - 1

		// Set a default callback that errors out when not all types are resolved.
		err = engine.exposePublicSymbol(name, func(ctx context.Context, this any, arguments ...any) (any, error) {
			return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call %s due to unbound types", name), argTypes)
		}, &publicSymbolArgs)
		if err != nil {
			panic(fmt.Errorf("could not expose public symbol: %w", err))
		}

		// When all types are resolved, replace the callback with the actual implementation.
		err = engine.whenDependentTypesAreResolved([]int32{}, argTypes, func(argTypes []registeredType) ([]registeredType, error) {
			invokerArgsArray := []registeredType{argTypes[0] /* return value */, nil /* no class 'this'*/}
			invokerArgsArray = append(invokerArgsArray, argTypes[1:]... /* actual params */)

			err = engine.replacePublicSymbol(name, engine.craftInvokerFunction(name, invokerArgsArray, nil /* no class 'this'*/, invokerFunc, fn, isAsync), &publicSymbolArgs)
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

const FunctionEmbindRegisterClass = "_embind_register_class"

var EmvalRegisterClass = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterClass,
	Name:       FunctionEmbindRegisterClass,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawType", "rawPointerType", "rawConstPointerType", "baseClassRawType", "getActualTypeSignature", "getActualType", "upcastSignature", "upcast", "downcastSignature", "downcast", "name", "destructorSignature", "rawDestructor"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		rawType := api.DecodeI32(stack[0])
		rawPointerType := api.DecodeI32(stack[1])
		rawConstPointerType := api.DecodeI32(stack[2])
		baseClassRawType := api.DecodeI32(stack[3])
		getActualTypeSignature := api.DecodeI32(stack[4])
		getActualType := api.DecodeI32(stack[5])
		upcastSignature := api.DecodeI32(stack[6])
		upcast := api.DecodeI32(stack[7])
		downcastSignature := api.DecodeI32(stack[8])
		downcast := api.DecodeI32(stack[9])
		namePtr := api.DecodeI32(stack[10])
		destructorSignature := api.DecodeI32(stack[11])
		rawDestructor := api.DecodeI32(stack[12])

		name, err := engine.readCString(uint32(namePtr))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		getActualTypeFunc, err := engine.newInvokeFunc(getActualTypeSignature, getActualType)
		if err != nil {
			panic(fmt.Errorf("could not read getActualType: %w", err))
		}

		var upcastFunc api.Function
		if upcast > 0 {
			upcastFunc, err = engine.newInvokeFunc(upcastSignature, upcast)
			if err != nil {
				panic(fmt.Errorf("could not read upcast: %w", err))
			}
		}

		var downcastFunc api.Function
		if downcast > 0 {
			downcastFunc, err = engine.newInvokeFunc(downcastSignature, downcast)
			if err != nil {
				panic(fmt.Errorf("could not read downcast: %w", err))
			}
		}

		rawDestructorFunc, err := engine.newInvokeFunc(destructorSignature, rawDestructor)
		if err != nil {
			panic(fmt.Errorf("could not read rawDestructor: %w", err))
		}

		legalFunctionName := engine.makeLegalFunctionName(name)

		// Set a default callback that errors out when not all types are resolved.
		err = engine.exposePublicSymbol(legalFunctionName, func(ctx context.Context, this any, arguments ...any) (any, error) {
			return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call %s due to unbound types", name), []int32{baseClassRawType})
		}, nil)
		if err != nil {
			panic(fmt.Errorf("could not expose public symbol: %w", err))
		}

		dependentTypes := []int32{}
		if baseClassRawType > 0 {
			dependentTypes = append(dependentTypes, baseClassRawType)
		}

		err = engine.whenDependentTypesAreResolved([]int32{rawType, rawPointerType, rawConstPointerType}, dependentTypes, func(resolvedTypes []registeredType) ([]registeredType, error) {
			existingClass, ok := engine.registeredClasses[name]
			if ok {
				if existingClass.baseType.rawType != 0 {
					return nil, fmt.Errorf("could not register class %s, already registered as raw type %d", name, existingClass.baseType.rawType)
				}
			} else {
				engine.registeredClasses[name] = &classType{
					baseType: baseType{
						rawType: rawType,
						name:    name,
					},
					pureVirtualFunctions: []string{},
					methods:              map[string]*publicSymbol{},
					properties:           map[string]*classProperty{},
				}
			}

			engine.registeredClasses[name].rawDestructor = rawDestructorFunc
			engine.registeredClasses[name].getActualType = getActualTypeFunc
			engine.registeredClasses[name].upcast = upcastFunc
			engine.registeredClasses[name].downcast = downcastFunc

			if baseClassRawType > 0 {
				engine.registeredClasses[name].baseClass = resolvedTypes[0].(*classType)
				if engine.registeredClasses[name].baseClass.derivedClasses == nil {
					engine.registeredClasses[name].baseClass.derivedClasses = []*classType{engine.registeredClasses[name]}
				} else {
					engine.registeredClasses[name].baseClass.derivedClasses = append(engine.registeredClasses[name].baseClass.derivedClasses, engine.registeredClasses[name])
				}
			}

			referenceConverter := &registeredPointerType{
				baseType: baseType{
					argPackAdvance: 8,
					name:           name,
				},
				registeredClass: engine.registeredClasses[name],
				isReference:     true,
				isConst:         false,
				isSmartPointer:  false,
			}

			pointerConverter := &registeredPointerType{
				baseType: baseType{
					argPackAdvance: 8,
					name:           name + "*",
				},
				registeredClass: engine.registeredClasses[name],
				isReference:     false,
				isConst:         false,
				isSmartPointer:  false,
			}

			constPointerConverter := &registeredPointerType{
				baseType: baseType{
					argPackAdvance: 8,
					name:           name + " const*",
				},
				registeredClass: engine.registeredClasses[name],
				isReference:     false,
				isConst:         true,
				isSmartPointer:  false,
			}

			engine.registeredPointers[rawType] = &registeredPointer{
				pointerType:      pointerConverter,
				constPointerType: constPointerConverter,
			}

			err := engine.registeredClasses[name].validate()
			if err != nil {
				return nil, err
			}

			err = engine.replacePublicSymbol(legalFunctionName, func(ctx context.Context, this any, arguments ...any) (any, error) {
				// @todo: implement me somehow?
				// if (Object.getPrototypeOf(this) !== instancePrototype) {
				// 	throw new BindingError("Use 'new' to construct " + name);
				// }

				if engine.registeredClasses[name].constructors == nil {
					return nil, fmt.Errorf("%s has no accessible constructor", name)
				}

				fn, ok := engine.registeredClasses[name].constructors[int32(len(arguments))]
				if !ok {
					availableLengths := []string{}
					for i := range engine.registeredClasses[name].constructors {
						availableLengths = append(availableLengths, strconv.Itoa(int(i)))
					}
					return nil, fmt.Errorf("tried to invoke ctor of %s with invalid number of parameters (%d) - expected (%s) parameters instead", name, len(arguments), strings.Join(availableLengths, " or "))
				}

				return fn(ctx, mod, this, arguments)
			}, nil)

			if err != nil {
				panic(fmt.Errorf("could not replace public symbol: %w", err))
			}

			return []registeredType{referenceConverter, pointerConverter, constPointerConverter}, nil
		})

		if err != nil {
			panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
		}
	})},
}

const FunctionEmbindRegisterClassConstructor = "_embind_register_class_constructor"

var EmbindRegisterClassConstructor = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterClassConstructor,
	Name:       FunctionEmbindRegisterClassConstructor,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"rawClassType", "argCount", "rawArgTypesAddr", "invokerSignature", "invoker", "rawConstructor"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		rawClassType := api.DecodeI32(stack[0])
		argCount := api.DecodeI32(stack[1])
		rawArgTypesAddr := api.DecodeI32(stack[2])
		invokerSignature := api.DecodeI32(stack[3])
		invoker := api.DecodeI32(stack[4])
		rawConstructor := api.DecodeI32(stack[5])

		rawArgTypes, err := engine.heap32VectorToArray(argCount, rawArgTypesAddr)
		if err != nil {
			panic(fmt.Errorf("could not read arg types: %w", err))
		}

		invokerFunc, err := engine.newInvokeFunc(invokerSignature, invoker)
		if err != nil {
			panic(fmt.Errorf("could not create invoke func: %w", err))
		}

		err = engine.whenDependentTypesAreResolved([]int32{}, []int32{rawClassType}, func(resolvedTypes []registeredType) ([]registeredType, error) {
			classType := resolvedTypes[0].(*registeredPointerType)
			humanName := "constructor " + classType.name

			if classType.registeredClass.constructors == nil {
				classType.registeredClass.constructors = map[int32]publicSymbolFn{}
			}

			if _, ok := classType.registeredClass.constructors[argCount-1]; ok {
				return nil, fmt.Errorf("cannot register multiple constructors with identical number of parameters (%d) for class '%s'! Overload resolution is currently only performed using the parameter count, not actual type info", argCount-1, classType.name)
			}

			classType.registeredClass.constructors[argCount-1] = func(ctx context.Context, this any, arguments ...any) (any, error) {
				return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call %s due to unbound types", classType.name), rawArgTypes)
			}

			err := engine.whenDependentTypesAreResolved([]int32{}, rawArgTypes, func(argTypes []registeredType) ([]registeredType, error) {
				// Insert empty slot for context type (argTypes[1]).
				newArgtypes := []registeredType{argTypes[0], nil}
				if len(argTypes) > 1 {
					newArgtypes = append(newArgtypes, argTypes[1:]...)
				}

				classType.registeredClass.constructors[argCount-1] = engine.craftInvokerFunction(humanName, newArgtypes, nil, invokerFunc, rawConstructor, false)
				return []registeredType{}, err
			})

			return []registeredType{}, err
		})

		if err != nil {
			panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
		}
	})},
}

const FunctionEmbindRegisterClassFunction = "_embind_register_class_function"

var EmbindRegisterClassFunction = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterClassFunction,
	Name:       FunctionEmbindRegisterClassFunction,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{
		"rawClassType",
		"methodName",
		"argCount",
		"rawArgTypesAddr", // [ReturnType, ThisType, Args...]
		"invokerSignature",
		"rawInvoker",
		"context",
		"isPureVirtual",
		"isAsync",
	},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		rawClassType := api.DecodeI32(stack[0])
		methodNamePtr := api.DecodeI32(stack[1])
		argCount := api.DecodeI32(stack[2])
		rawArgTypesAddr := api.DecodeI32(stack[3])
		invokerSignature := api.DecodeI32(stack[4])
		rawInvoker := api.DecodeI32(stack[5])
		contextPtr := api.DecodeI32(stack[6])
		isPureVirtual := api.DecodeI32(stack[7])
		isAsync := api.DecodeI32(stack[8])

		rawArgTypes, err := engine.heap32VectorToArray(argCount, rawArgTypesAddr)
		if err != nil {
			panic(fmt.Errorf("could not read arg types: %w", err))
		}

		methodName, err := engine.readCString(uint32(methodNamePtr))
		if err != nil {
			panic(fmt.Errorf("could not read method name: %w", err))
		}

		rawInvokerFunc, err := engine.newInvokeFunc(invokerSignature, rawInvoker)
		if err != nil {
			panic(fmt.Errorf("could not create raw invoke func: %w", err))
		}

		err = engine.whenDependentTypesAreResolved([]int32{}, []int32{rawClassType}, func(classTypes []registeredType) ([]registeredType, error) {
			classType := classTypes[0].(*registeredPointerType)
			humanName := classType.Name() + "." + methodName

			if strings.HasPrefix(methodName, "@@") {
				methodName = engine.emvalEngine.globals[strings.TrimPrefix(methodName, "@@")].(string)
			}

			if isPureVirtual > 0 {
				classType.registeredClass.pureVirtualFunctions = append(classType.registeredClass.pureVirtualFunctions, methodName)
			}

			unboundTypesHandler := &publicSymbol{
				fn: func(ctx context.Context, this any, arguments ...any) (any, error) {
					return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call %s due to unbound types", humanName), rawArgTypes)
				},
			}

			newMethodArgCount := argCount - 2
			existingMethod, ok := classType.registeredClass.methods[methodName]
			if !ok || (existingMethod.overloadTable == nil && existingMethod.className != classType.name && *existingMethod.argCount == newMethodArgCount) {
				// This is the first overload to be registered, OR we are replacing a
				// function in the base class with a function in the derived class.
				unboundTypesHandler.argCount = &newMethodArgCount
				unboundTypesHandler.className = classType.name
				classType.registeredClass.methods[methodName] = unboundTypesHandler
			} else {
				// There was an existing function with the same name registered. Set up
				// a function overload routing table.
				engine.ensureOverloadTable(classType.registeredClass.methods, methodName, humanName)
				classType.registeredClass.methods[methodName].overloadTable[argCount-2] = unboundTypesHandler
			}

			err = engine.whenDependentTypesAreResolved([]int32{}, rawArgTypes, func(argTypes []registeredType) ([]registeredType, error) {
				memberFunction := &publicSymbol{
					fn: engine.craftInvokerFunction(humanName, argTypes, classType, rawInvokerFunc, contextPtr, isAsync > 0),
				}

				// Replace the initial unbound-handler-stub function with the appropriate member function, now that all types
				// are resolved. If multiple overloads are registered for this function, the function goes into an overload table.
				if classType.registeredClass.methods[methodName].overloadTable == nil {
					// Set argCount in case an overload is registered later
					memberFunction.argCount = &newMethodArgCount
					classType.registeredClass.methods[methodName] = memberFunction
				} else {
					classType.registeredClass.methods[methodName].overloadTable[argCount-2] = memberFunction
				}

				return []registeredType{}, nil
			})

			return []registeredType{}, err
		})

		if err != nil {
			panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
		}
	})},
}

const FunctionEmbindRegisterClassClassFunction = "_embind_register_class_class_function"

var EmbindRegisterClassClassFunction = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterClassClassFunction,
	Name:       FunctionEmbindRegisterClassClassFunction,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{
		"rawClassType",
		"methodName",
		"argCount",
		"rawArgTypesAddr", // [ReturnType, ThisType, Args...]
		"invokerSignature",
		"rawInvoker",
		"fn",
		"isAsync",
	},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		rawClassType := api.DecodeI32(stack[0])
		methodNamePtr := api.DecodeI32(stack[1])
		argCount := api.DecodeI32(stack[2])
		rawArgTypesAddr := api.DecodeI32(stack[3])
		invokerSignature := api.DecodeI32(stack[4])
		rawInvoker := api.DecodeI32(stack[5])
		fn := api.DecodeI32(stack[6])
		isAsync := api.DecodeI32(stack[7])

		rawArgTypes, err := engine.heap32VectorToArray(argCount, rawArgTypesAddr)
		if err != nil {
			panic(fmt.Errorf("could not read arg types: %w", err))
		}

		methodName, err := engine.readCString(uint32(methodNamePtr))
		if err != nil {
			panic(fmt.Errorf("could not read method name: %w", err))
		}

		rawInvokerFunc, err := engine.newInvokeFunc(invokerSignature, rawInvoker)
		if err != nil {
			panic(fmt.Errorf("could not create raw invoke func: %w", err))
		}

		err = engine.whenDependentTypesAreResolved([]int32{}, []int32{rawClassType}, func(classTypes []registeredType) ([]registeredType, error) {
			classType := classTypes[0].(*registeredPointerType)
			humanName := classType.Name() + "." + methodName

			if strings.HasPrefix(methodName, "@@") {
				methodName = engine.emvalEngine.globals[strings.TrimPrefix(methodName, "@@")].(string)
			}

			unboundTypesHandler := &publicSymbol{
				fn: func(ctx context.Context, this any, arguments ...any) (any, error) {
					return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call %s due to unbound types", humanName), rawArgTypes)
				},
			}

			newArgCount := argCount - 1
			_, ok := classType.registeredClass.methods[methodName]
			if !ok {
				// This is the first function to be registered with this name.
				unboundTypesHandler.argCount = &newArgCount
				classType.registeredClass.methods[methodName] = unboundTypesHandler
			} else {
				// There was an existing function with the same name registered. Set up
				// a function overload routing table.
				engine.ensureOverloadTable(classType.registeredClass.methods, methodName, humanName)
				classType.registeredClass.methods[methodName].overloadTable[argCount-1] = unboundTypesHandler
			}

			err = engine.whenDependentTypesAreResolved([]int32{}, rawArgTypes, func(argTypes []registeredType) ([]registeredType, error) {
				invokerArgsArray := []registeredType{argTypes[0], nil}
				invokerArgsArray = append(invokerArgsArray, argTypes[1:]...)

				memberFunction := &publicSymbol{
					fn: engine.craftInvokerFunction(humanName, invokerArgsArray, nil, rawInvokerFunc, fn, isAsync > 0),
				}

				// Replace the initial unbound-handler-stub function with the appropriate member function, now that all types
				// are resolved. If multiple overloads are registered for this function, the function goes into an overload table.
				if classType.registeredClass.methods[methodName].overloadTable == nil {
					// Set argCount in case an overload is registered later
					memberFunction.argCount = &newArgCount
					classType.registeredClass.methods[methodName] = memberFunction
				} else {
					classType.registeredClass.methods[methodName].overloadTable[argCount-1] = memberFunction
				}

				if classType.registeredClass.derivedClasses != nil {
					for i := range classType.registeredClass.derivedClasses {
						derivedClass := classType.registeredClass.derivedClasses[i]
						_, ok := derivedClass.methods[methodName]
						if !ok {
							// TODO: Add support for overloads
							derivedClass.methods[methodName] = memberFunction
						}
					}
				}

				return []registeredType{}, nil
			})

			return []registeredType{}, err
		})
		if err != nil {
			panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
		}
	})},
}

const FunctionEmbindRegisterClassProperty = "_embind_register_class_property"

var EmbindRegisterClassProperty = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterClassProperty,
	Name:       FunctionEmbindRegisterClassProperty,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{
		"classType",
		"fieldName",
		"getterReturnType",
		"getterSignature",
		"getter",
		"getterContext",
		"setterArgumentType",
		"setterSignature",
		"setter",
		"setterContext",
	},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		classType := api.DecodeI32(stack[0])
		fieldNamePtr := api.DecodeI32(stack[1])
		getterReturnType := api.DecodeI32(stack[2])
		getterSignature := api.DecodeI32(stack[3])
		getter := api.DecodeI32(stack[4])
		getterContext := api.DecodeI32(stack[5])
		setterArgumentType := api.DecodeI32(stack[6])
		setterSignature := api.DecodeI32(stack[7])
		setter := api.DecodeI32(stack[8])
		setterContext := api.DecodeI32(stack[9])

		fieldName, err := engine.readCString(uint32(fieldNamePtr))
		if err != nil {
			panic(fmt.Errorf("could not read method name: %w", err))
		}

		getterFunc, err := engine.newInvokeFunc(getterSignature, getter)
		if err != nil {
			panic(fmt.Errorf("could not create raw invoke func: %w", err))
		}

		err = engine.whenDependentTypesAreResolved([]int32{}, []int32{classType}, func(classTypes []registeredType) ([]registeredType, error) {
			classType := classTypes[0].(*registeredPointerType)
			humanName := classType.Name() + "." + fieldName

			desc := &classProperty{
				get: func(ctx context.Context, mod api.Module, this any) (any, error) {
					return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot access %s due to unbound types", humanName), []int32{getterReturnType, setterArgumentType})
				},
				enumerable:   true,
				configurable: true,
			}

			if setter > 0 {
				desc.set = func(ctx context.Context, mod api.Module, this any, v any) error {
					return engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot access %s due to unbound types", humanName), []int32{getterReturnType, setterArgumentType})
				}
			} else {
				desc.set = func(ctx context.Context, mod api.Module, this any, v any) error {
					return fmt.Errorf("%s is a read-only property", humanName)
				}
			}

			classType.registeredClass.properties[fieldName] = desc

			requiredTypes := []int32{getterReturnType}
			if setter > 0 {
				requiredTypes = append(requiredTypes, getterReturnType)
			}

			err = engine.whenDependentTypesAreResolved([]int32{}, requiredTypes, func(types []registeredType) ([]registeredType, error) {
				getterReturnType := types[0]
				desc := &classProperty{
					get: func(ctx context.Context, mod api.Module, this any) (any, error) {
						ptr, err := engine.validateThis(ctx, this, classType, humanName+" getter")
						if err != nil {
							return nil, err
						}

						res, err := getterFunc.Call(ctx, api.EncodeI32(getterContext), api.EncodeU32(ptr))
						if err != nil {
							return nil, err
						}
						return getterReturnType.FromWireType(ctx, mod, res[0])
					},
					enumerable: true,
				}

				if setter > 0 {
					setterFunc, err := engine.newInvokeFunc(setterSignature, setter)
					if err != nil {
						return nil, err
					}

					setterArgumentType := types[1]

					desc.set = func(ctx context.Context, mod api.Module, this any, v any) error {
						ptr, err := engine.validateThis(ctx, this, classType, humanName+" setter")
						if err != nil {
							return err
						}

						destructors := &[]*destructorFunc{}
						setterRes, err := setterArgumentType.ToWireType(ctx, mod, destructors, v)
						if err != nil {
							return err
						}

						_, err = setterFunc.Call(ctx, api.EncodeI32(setterContext), api.EncodeU32(ptr), setterRes)
						if err != nil {
							return err
						}

						err = engine.runDestructors(ctx, *destructors)
						if err != nil {
							return err
						}

						return nil
					}
				}

				classType.registeredClass.properties[fieldName] = desc

				return []registeredType{}, err
			})

			return []registeredType{}, err
		})
		if err != nil {
			panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
		}
	})},
}

const FunctionEmbindRegisterValueArray = "_embind_register_value_array"

var EmbindRegisterValueArray = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterValueArray,
	Name:       FunctionEmbindRegisterValueArray,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{
		"rawType",
		"name",
		"constructorSignature",
		"rawConstructor",
		"destructorSignature",
		"rawDestructor",
	},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		rawType := api.DecodeI32(stack[0])
		namePtr := api.DecodeI32(stack[1])
		constructorSignature := api.DecodeI32(stack[2])
		rawConstructor := api.DecodeI32(stack[3])
		destructorSignature := api.DecodeI32(stack[4])
		rawDestructor := api.DecodeI32(stack[5])

		name, err := engine.readCString(uint32(namePtr))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		rawConstructorFunc, err := engine.newInvokeFunc(constructorSignature, rawConstructor)
		if err != nil {
			panic(fmt.Errorf("could not create raw invoke func: %w", err))
		}

		rawDestructorFunc, err := engine.newInvokeFunc(destructorSignature, rawDestructor)
		if err != nil {
			panic(fmt.Errorf("could not create raw invoke func: %w", err))
		}

		engine.registeredTuples[rawType] = &registeredTuple{
			name:           name,
			rawConstructor: rawConstructorFunc,
			rawDestructor:  rawDestructorFunc,
			elements:       []*registeredTupleElement{},
		}
	})},
}

const FunctionEmbindRegisterValueArrayElement = "_embind_register_value_array_element"

var EmbindRegisterValueArrayElement = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterValueArrayElement,
	Name:       FunctionEmbindRegisterValueArrayElement,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{
		"rawTupleType",
		"getterReturnType",
		"getterSignature",
		"getter",
		"getterContext",
		"setterArgumentType",
		"setterSignature",
		"setter",
		"setterContext",
	},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		rawTupleType := api.DecodeI32(stack[0])
		getterReturnType := api.DecodeI32(stack[1])
		getterSignature := api.DecodeI32(stack[2])
		getter := api.DecodeI32(stack[3])
		getterContext := api.DecodeI32(stack[4])
		setterArgumentType := api.DecodeI32(stack[5])
		setterSignature := api.DecodeI32(stack[6])
		setter := api.DecodeI32(stack[7])
		setterContext := api.DecodeI32(stack[8])

		getterFunc, err := engine.newInvokeFunc(getterSignature, getter)
		if err != nil {
			panic(fmt.Errorf("could not create raw invoke func: %w", err))
		}

		setterFunc, err := engine.newInvokeFunc(setterSignature, setter)
		if err != nil {
			panic(fmt.Errorf("could not create raw invoke func: %w", err))
		}

		engine.registeredTuples[rawTupleType].elements = append(engine.registeredTuples[rawTupleType].elements, &registeredTupleElement{
			getterReturnType:   getterReturnType,
			getter:             getterFunc,
			getterContext:      getterContext,
			setterArgumentType: setterArgumentType,
			setter:             setterFunc,
			setterContext:      setterContext,
		})
	})},
}

const FunctionEmbindFinalizeValueArray = "_embind_finalize_value_array"

var EmbindFinalizeValueArray = &wasm.HostFunc{
	ExportName: FunctionEmbindFinalizeValueArray,
	Name:       FunctionEmbindFinalizeValueArray,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames: []string{
		"rawTupleType",
	},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		rawTupleType := api.DecodeI32(stack[0])
		reg := engine.registeredTuples[rawTupleType]
		delete(engine.registeredTuples, rawTupleType)
		elements := reg.elements
		elementsLength := len(elements)

		elementTypes := []int32{}
		for i := range elements {
			elementTypes = append(elementTypes, elements[i].getterReturnType)
			elementTypes = append(elementTypes, elements[i].setterArgumentType)
		}

		err := engine.whenDependentTypesAreResolved([]int32{rawTupleType}, elementTypes, func(types []registeredType) ([]registeredType, error) {
			for i := range elements {
				getterReturnType := types[i]
				getter := elements[i].getter
				getterContext := elements[i].getterContext
				setterArgumentType := types[i+elementsLength]
				setter := elements[i].setter
				setterContext := elements[i].setterContext
				elements[i].read = func(ctx context.Context, mod api.Module, ptr int32) (any, error) {
					res, err := getter.Call(ctx, api.EncodeI32(getterContext), api.EncodeI32(ptr))
					if err != nil {
						return nil, err
					}
					return getterReturnType.FromWireType(ctx, mod, res[0])
				}
				elements[i].write = func(ctx context.Context, mod api.Module, ptr int32, o any) error {
					destructors := &[]*destructorFunc{}
					res, err := setterArgumentType.ToWireType(ctx, mod, destructors, o)
					if err != nil {
						return err
					}

					_, err = setter.Call(ctx, api.EncodeI32(setterContext), api.EncodeI32(ptr), res)
					if err != nil {
						return err
					}

					err = engine.runDestructors(ctx, *destructors)
					if err != nil {
						return err
					}

					return nil
				}
			}

			return []registeredType{
				&arrayType{
					baseType: baseType{
						name:           reg.name,
						argPackAdvance: 8,
					},
					reg:            reg,
					elementsLength: elementsLength,
				},
			}, nil
		})
		if err != nil {
			panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
		}
	})},
}

const FunctionEmbindRegisterValueObject = "_embind_register_value_object"

var EmbindRegisterValueObject = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterValueObject,
	Name:       FunctionEmbindRegisterValueObject,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{
		"rawType",
		"name",
		"constructorSignature",
		"rawConstructor",
		"destructorSignature",
		"rawDestructor",
	},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		rawType := api.DecodeI32(stack[0])
		namePtr := api.DecodeI32(stack[1])
		constructorSignature := api.DecodeI32(stack[2])
		rawConstructor := api.DecodeI32(stack[3])
		destructorSignature := api.DecodeI32(stack[4])
		rawDestructor := api.DecodeI32(stack[5])

		name, err := engine.readCString(uint32(namePtr))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		rawConstructorFunc, err := engine.newInvokeFunc(constructorSignature, rawConstructor)
		if err != nil {
			panic(fmt.Errorf("could not create raw invoke func: %w", err))
		}

		rawDestructorFunc, err := engine.newInvokeFunc(destructorSignature, rawDestructor)
		if err != nil {
			panic(fmt.Errorf("could not create raw invoke func: %w", err))
		}

		engine.registeredObjects[rawType] = &registeredObject{
			name:           name,
			rawConstructor: rawConstructorFunc,
			rawDestructor:  rawDestructorFunc,
			fields:         []*registeredObjectField{},
		}
	})},
}

const FunctionEmbindRegisterValueObjectField = "_embind_register_value_object_field"

var EmbindRegisterValueObjectField = &wasm.HostFunc{
	ExportName: FunctionEmbindRegisterValueObjectField,
	Name:       FunctionEmbindRegisterValueObjectField,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{
		"structType",
		"fieldName",
		"getterReturnType",
		"getterSignature",
		"getter",
		"getterContext",
		"setterArgumentType",
		"setterSignature",
		"setter",
		"setterContext",
	},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		structType := api.DecodeI32(stack[0])
		fieldNamePtr := api.DecodeI32(stack[1])
		getterReturnType := api.DecodeI32(stack[2])
		getterSignature := api.DecodeI32(stack[3])
		getter := api.DecodeI32(stack[4])
		getterContext := api.DecodeI32(stack[5])
		setterArgumentType := api.DecodeI32(stack[6])
		setterSignature := api.DecodeI32(stack[7])
		setter := api.DecodeI32(stack[8])
		setterContext := api.DecodeI32(stack[9])

		fieldName, err := engine.readCString(uint32(fieldNamePtr))
		if err != nil {
			panic(fmt.Errorf("could not read field name: %w", err))
		}

		getterFunc, err := engine.newInvokeFunc(getterSignature, getter)
		if err != nil {
			panic(fmt.Errorf("could not create raw invoke func: %w", err))
		}

		setterFunc, err := engine.newInvokeFunc(setterSignature, setter)
		if err != nil {
			panic(fmt.Errorf("could not create raw invoke func: %w", err))
		}

		engine.registeredObjects[structType].fields = append(engine.registeredObjects[structType].fields, &registeredObjectField{
			fieldName:          fieldName,
			getterReturnType:   getterReturnType,
			getter:             getterFunc,
			getterContext:      getterContext,
			setterArgumentType: setterArgumentType,
			setter:             setterFunc,
			setterContext:      setterContext,
		})
	})},
}

const FunctionEmbindFinalizeValueObject = "_embind_finalize_value_object"

var EmbindFinalizeValueObject = &wasm.HostFunc{
	ExportName: FunctionEmbindFinalizeValueObject,
	Name:       FunctionEmbindFinalizeValueObject,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames: []string{
		"structType",
	},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)
		structType := api.DecodeI32(stack[0])
		reg := engine.registeredObjects[structType]
		delete(engine.registeredObjects, structType)
		fieldRecords := reg.fields

		fieldTypes := []int32{}
		for i := range fieldRecords {
			fieldTypes = append(fieldTypes, fieldRecords[i].getterReturnType)
			fieldTypes = append(fieldTypes, fieldRecords[i].setterArgumentType)
		}

		err := engine.whenDependentTypesAreResolved([]int32{structType}, fieldTypes, func(types []registeredType) ([]registeredType, error) {
			for i := range fieldRecords {
				getterReturnType := types[i]
				getter := fieldRecords[i].getter
				getterContext := fieldRecords[i].getterContext
				setterArgumentType := types[i+len(fieldRecords)]
				setter := fieldRecords[i].setter
				setterContext := fieldRecords[i].setterContext

				fieldRecords[i].read = func(ctx context.Context, mod api.Module, ptr int32) (any, error) {
					res, err := getter.Call(ctx, api.EncodeI32(getterContext), api.EncodeI32(ptr))
					if err != nil {
						return nil, err
					}
					return getterReturnType.FromWireType(ctx, mod, res[0])
				}
				fieldRecords[i].write = func(ctx context.Context, mod api.Module, ptr int32, o any) error {
					destructors := &[]*destructorFunc{}
					res, err := setterArgumentType.ToWireType(ctx, mod, destructors, o)
					if err != nil {
						return err
					}

					_, err = setter.Call(ctx, api.EncodeI32(setterContext), api.EncodeI32(ptr), res)
					if err != nil {
						return err
					}

					err = engine.runDestructors(ctx, *destructors)
					if err != nil {
						return err
					}

					return nil
				}
			}

			return []registeredType{
				&objectType{
					baseType: baseType{
						name:           reg.name,
						argPackAdvance: 8,
					},
					reg: reg,
				},
			}, nil
		})
		if err != nil {
			panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
		}
	})},
}
