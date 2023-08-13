package embind

import (
	"context"
	"fmt"
	"log"
	"strings"

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
		err = engine.exposePublicSymbol(name, func(ctx context.Context, mod api.Module, this any, arguments ...any) (any, error) {
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
		log.Printf("memory_view: %d", rawType)
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
