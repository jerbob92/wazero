package embind

import (
	"context"
	"errors"
	"fmt"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
	"strconv"
	"strings"
)

func (e *engine) CallFunction(ctx context.Context, name string, arguments ...any) (any, error) {
	_, ok := e.publicSymbols[name]
	if !ok {
		return nil, fmt.Errorf("could not find public symbol %s", name)
	}

	res, err := e.publicSymbols[name].fn(ctx, e.mod, nil, arguments...)
	if err != nil {
		return nil, fmt.Errorf("error while calling embind function %s: %w", name, err)
	}

	return res, nil
}

func (e *engine) embind__requireFunction(signaturePtr, rawInvoker int32) api.Function {
	// Not used in Wazero.
	//signature, err := readCString(mod, uint32(signaturePtr))
	//if err != nil {
	//	panic(fmt.Errorf("could not read signature: %w", err))
	//}

	// This needs copy (not reslice) because the stack is reused for results.
	// Consider invoke_i (zero arguments, one result): index zero (tableOffset)
	// is needed to store the result.
	tableOffset := wasm.Index(rawInvoker) // position in the module's only table.

	m := e.mod.(*wasm.ModuleInstance)

	// Lookup the table index we will call.
	t := m.Tables[0] // Note: Emscripten doesn't use multiple tables

	// We do not know the function type ID and also don't really care.
	f, err := m.Engine.LookupFunction(t, nil, tableOffset)
	if err != nil {
		panic(err)
	}

	return f
}

func (e *engine) heap32VectorToArray(count, firstElement int32) ([]int32, error) {
	array := make([]int32, count)
	for i := int32(0); i < count; i++ {
		val, ok := e.mod.Memory().ReadUint32Le(uint32(firstElement + (i * 4)))
		if !ok {
			return nil, errors.New("could not read uint32")
		}
		array[i] = int32(val)
	}
	return array, nil
}

func (e *engine) registerType(rawType int32, registeredInstance *registeredType, options *registerTypeOptions) error {
	name := registeredInstance.name
	if rawType == 0 {
		return fmt.Errorf("type \"%s\" must have a positive integer typeid pointer", name)
	}

	_, ok := e.registeredTypes[rawType]
	if ok {
		if options != nil && options.ignoreDuplicateRegistrations {
			return nil
		} else {
			return fmt.Errorf("cannot register type '%s' twice", name)
		}
	}

	e.registeredTypes[rawType] = registeredInstance
	delete(e.typeDependencies, rawType)

	callbacks, ok := e.awaitingDependencies[rawType]
	if ok {
		delete(e.awaitingDependencies, rawType)
		for i := range callbacks {
			err := callbacks[i].cb()
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *engine) ensureOverloadTable(methodName, humanName string) {
	if e.publicSymbols[methodName].overloadTable == nil {
		prevFunc := e.publicSymbols[methodName].fn
		prevArgCount := e.publicSymbols[methodName].argCount

		// Inject an overload resolver function that routes to the appropriate overload based on the number of arguments.
		e.publicSymbols[methodName].fn = func(ctx context.Context, mod api.Module, this any, arguments ...any) (any, error) {
			_, ok := e.publicSymbols[methodName].overloadTable[int32(len(arguments))]
			if !ok {
				possibleOverloads := make([]string, len(e.publicSymbols[methodName].overloadTable))
				for i := range e.publicSymbols[methodName].overloadTable {
					possibleOverloads = append(possibleOverloads, strconv.Itoa(int(i)))
				}
				return nil, fmt.Errorf("function '%s' called with an invalid number of arguments (%d) - expects one of (%s)", humanName, len(arguments), strings.Join(possibleOverloads, ", "))
			}

			return e.publicSymbols[methodName].overloadTable[int32(len(arguments))].fn(ctx, mod, this, arguments)
		}

		// Move the previous function into the overload table.
		e.publicSymbols[methodName].overloadTable = map[int32]*publicSymbol{}
		e.publicSymbols[methodName].overloadTable[prevArgCount] = &publicSymbol{
			argCount: prevArgCount,
			fn:       prevFunc,
		}
	}
}

func (e *engine) exposePublicSymbol(name string, value func(ctx context.Context, mod api.Module, this any, arguments ...any) (any, error), numArguments int32) {
	_, ok := e.publicSymbols[name]
	if ok {
		_, ok = e.publicSymbols[name].overloadTable[numArguments]
		if ok {
			panic(fmt.Errorf("cannot register public name '%s' twice", name))
		}

		e.ensureOverloadTable(name, name)

		// What does this actually do?
		//if (Module.hasOwnProperty(numArguments)) {
		//	throwBindingError(`Cannot register multiple overloads of a function with the same number of arguments (${numArguments})!`);
		//}

		// Add the new function into the overload table.
		e.publicSymbols[name].overloadTable[numArguments] = &publicSymbol{
			argCount: numArguments,
			fn:       value,
		}
	} else {
		e.publicSymbols[name] = &publicSymbol{
			argCount: numArguments,
			fn:       value,
		}
	}
}

func (e *engine) replacePublicSymbol(name string, value func(ctx context.Context, mod api.Module, this any, arguments ...any) (any, error), numArguments int32) error {
	_, ok := e.publicSymbols[name]
	if !ok {
		return fmt.Errorf("tried to replace a nonexistant public symbol %s", name)
	}

	// If there's an overload table for this symbol, replace the symbol in the overload table instead.
	if e.publicSymbols[name].overloadTable != nil && numArguments > 0 {
		e.publicSymbols[name].overloadTable[numArguments] = &publicSymbol{
			argCount: numArguments,
			fn:       value,
		}
	} else {
		e.publicSymbols[name] = &publicSymbol{
			argCount: numArguments,
			fn:       value,
		}
	}

	return nil
}

func (e *engine) whenDependentTypesAreResolved(myTypes, dependentTypes []int32, getTypeConverters func([]*registeredType) ([]*registeredType, error)) error {
	for i := range myTypes {
		e.typeDependencies[myTypes[i]] = dependentTypes
	}

	onComplete := func(typeConverters []*registeredType) error {
		var myTypeConverters, err = getTypeConverters(typeConverters)
		if err != nil {
			return err
		}

		if len(myTypeConverters) != len(myTypes) {
			return fmt.Errorf("mismatched type converter count")
		}

		for i := range myTypes {
			err := e.registerType(myTypes[i], myTypeConverters[i], nil)
			if err != nil {
				return err
			}
		}

		return nil
	}

	typeConverters := make([]*registeredType, len(dependentTypes))
	unregisteredTypes := make([]int32, 0)
	registered := 0

	for i := range dependentTypes {
		// Make a local var to use it inside the callback.
		myI := i
		dt := dependentTypes[i]
		_, ok := e.registeredTypes[dt]
		if ok {
			typeConverters[i] = e.registeredTypes[dt]
		} else {
			unregisteredTypes = append(unregisteredTypes, dt)
			_, ok := e.awaitingDependencies[dt]
			if !ok {
				e.awaitingDependencies[dt] = []*awaitingDependency{}
			}

			e.awaitingDependencies[dt] = append(e.awaitingDependencies[dt], &awaitingDependency{
				cb: func() error {
					typeConverters[myI] = e.registeredTypes[dt]
					registered++
					if registered == len(unregisteredTypes) {
						err := onComplete(typeConverters)
						if err != nil {
							return err
						}
					}

					return nil
				},
			})
		}
	}

	if 0 == len(unregisteredTypes) {
		err := onComplete(typeConverters)
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *engine) craftInvokerFunction(humanName string, argTypes []*registeredType, classType *classType, cppInvokerFunc api.Function, cppTargetFunc int32, isAsync bool) func(ctx context.Context, mod api.Module, this any, arguments ...any) (any, error) {
	// humanName: a human-readable string name for the function to be generated.
	// argTypes: An array that contains the embind type objects for all types in the function signature.
	//    argTypes[0] is the type object for the function return value.
	//    argTypes[1] is the type object for function this object/class type, or null if not crafting an invoker for a class method.
	//    argTypes[2...] are the actual function parameters.
	// classType: The embind type object for the class to be bound, or null if this is not a method of a class.
	// cppInvokerFunc: JS Function object to the C++-side function that interops into C++ code.
	// cppTargetFunc: Function pointer (an integer to FUNCTION_TABLE) to the target C++ function the cppInvokerFunc will end up calling.
	// isAsync: Optional. If true, returns an async function. Async bindings are only supported with JSPI.
	argCount := len(argTypes)
	if argCount < 2 {
		panic(fmt.Errorf("argTypes array size mismatch! Must at least get return value and 'this' types"))
	}

	if isAsync {
		panic(fmt.Errorf("async bindings are only supported with JSPI"))
	}

	isClassMethodFunc := argTypes[1] != nil && classType != nil
	// Free functions with signature "void function()" do not need an invoker that marshalls between wire types.
	// TODO: This omits argument count check - enable only at -O3 or similar.
	//    if (ENABLE_UNSAFE_OPTS && argCount == 2 && argTypes[0].name == "void" && !isClassMethodFunc) {
	//       return FUNCTION_TABLE[fn];
	//    }

	// Determine if we need to use a dynamic stack to store the destructors for the function parameters.
	// TODO: Remove this completely once all function invokers are being dynamically generated.
	needsDestructorStack := false
	for i := 1; i < len(argTypes); i++ { // Skip return value at index 0 - it's not deleted here.
		if argTypes[i] != nil && argTypes[i].destructorFunction != nil { // The type does not define a destructor function - must use dynamic stack
			needsDestructorStack = true
			break
		}
	}

	returns := argTypes[0].name != "void"

	return func(ctx context.Context, mod api.Module, this any, arguments ...any) (any, error) {
		if len(arguments) != argCount-2 {
			return nil, fmt.Errorf("function %s called with %d arguments, expected %d args", humanName, len(arguments), argCount-2)
		}

		invoker := cppInvokerFunc
		fn := cppTargetFunc
		retType := argTypes[0]
		classParam := argTypes[1]

		var destructors *[]*destructorFunc

		if needsDestructorStack {
			destructors = &[]*destructorFunc{}
		}

		var thisWired uint64
		var err error

		if isClassMethodFunc {
			thisWired, err = classParam.toWireType(ctx, mod, destructors, this)
			if err != nil {
				return nil, fmt.Errorf("could not get wire type of class param: %w", err)
			}
		}

		argsWired := make([]uint64, argCount-2)
		for i := 0; i < argCount-2; i++ {
			argsWired[i], err = argTypes[i+2].toWireType(ctx, mod, destructors, arguments[i])
			if err != nil {
				return nil, fmt.Errorf("could not get wire type of argument %d (%s): %w", i, argTypes[i+2].name, err)
			}
		}

		callArgs := []uint64{api.EncodeI32(fn)}
		if isClassMethodFunc {
			callArgs = append(callArgs, thisWired)
		}
		callArgs = append(callArgs, argsWired...)

		res, err := invoker.Call(ctx, callArgs...)
		if err != nil {
			return nil, err
		}

		if needsDestructorStack {
			err = e.runDestructors(ctx, *destructors)
			if err != nil {
				return nil, err
			}
		} else {
			// Skip return value at index 0 - it's not deleted here. Also skip class type if not a method.
			startArg := 2
			if isClassMethodFunc {
				startArg = 1
			}
			for i := startArg; i < len(argTypes); i++ {
				if argTypes[i].destructorFunction != nil {
					err = argTypes[i].destructorFunction(ctx, mod, api.DecodeU32(callArgs[i]))
					if err != nil {
						return nil, err
					}
				}
			}
		}

		if returns {
			returnVal, err := retType.fromWireType(ctx, e.mod, res[0])
			if err != nil {
				return nil, fmt.Errorf("could not get wire type of return value (%s): %w", retType.name, err)
			}

			return returnVal, nil
		}

		return nil, nil
	}
}

type destructorFunc struct {
	function string
	args     []uint64
}

func (e *engine) runDestructors(ctx context.Context, destructors []*destructorFunc) error {
	for i := range destructors {
		_, err := e.mod.ExportedFunction(destructors[i].function).Call(ctx, destructors[i].args...)
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *engine) getShiftFromSize(size int32) (int32, error) {
	switch size {
	case 1:
		return 0, nil
	case 2:
		return 1, nil
	case 4:
		return 2, nil
	case 8:
		return 3, nil
	default:
		return 0, fmt.Errorf("unknown type size: %d", size)
	}
}

// readCString reads a C string by reading byte per byte until it sees a NULL
// byte which is used as a string terminator in C.
// @todo: limit this so we won't try to read too much when some mistake is made?
func (e *engine) readCString(addr uint32) (string, error) {
	var sb strings.Builder
	for {
		b, success := e.mod.Memory().ReadByte(addr)
		if !success {
			return "", errors.New("could not read C string data")
		}

		// Stop when we encounter nil terminator of Cstring
		if b == 0 {
			break
		}

		// Write byte to string builder.
		sb.WriteByte(b)

		// Move to next char.
		addr++
	}

	return sb.String(), nil
}

// checkRegisteredTypeDependencies recursively loops through types to return
// which types not have registered on the engine yet. The seen map is used to
// keep track which types has been seen so the same type isn't reported or
// checked twice.
func (e *engine) checkRegisteredTypeDependencies(typeToVisit int32, seen *map[int32]bool) []int32 {
	unboundTypes := make([]int32, 0)
	seenMap := *seen
	if seenMap[typeToVisit] {
		return nil
	}

	_, ok := e.registeredTypes[typeToVisit]
	if ok {
		return nil
	}

	_, ok = e.typeDependencies[typeToVisit]
	if ok {
		for i := range e.typeDependencies[typeToVisit] {
			newUnboundTypes := e.checkRegisteredTypeDependencies(e.typeDependencies[typeToVisit][i], seen)
			if newUnboundTypes != nil {
				unboundTypes = append(unboundTypes, newUnboundTypes...)
			}
		}
		return nil
	}

	unboundTypes = append(unboundTypes, typeToVisit)
	seenMap[typeToVisit] = true
	seen = &seenMap
	return unboundTypes
}

// getTypeName calls the Emscripten exported function __getTypeName to get a
// pointer to the C string that contains the type name.
func (e *engine) getTypeName(ctx context.Context, typeId int32) (string, error) {
	typeNameRes, err := e.mod.ExportedFunction("__getTypeName").Call(ctx, api.EncodeI32(typeId))
	if err != nil {
		return "", err
	}

	ptr := api.DecodeI32(typeNameRes[0])
	rv, err := e.readCString(uint32(ptr))
	if err != nil {
		return "", err
	}

	_, err = e.mod.ExportedFunction("free").Call(ctx, api.EncodeI32(ptr))
	if err != nil {
		return "", err
	}

	return rv, nil
}

// createUnboundTypeError generated the error for when not all required type
// dependencies are resolved. It will traverse the dependency tree and list all
// the missing types by name.
func (e *engine) createUnboundTypeError(ctx context.Context, message string, types []int32) error {
	unregisteredTypes := []int32{}

	// The seen map is used to keep tracks of the seen dependencies, so that if
	// two types have the same dependency, it won't be listed or traversed twice.
	seen := map[int32]bool{}

	// Loop through all required types.
	for i := range types {
		// If we have any unregistered types, add them to the list
		newUnregisteredTypes := e.checkRegisteredTypeDependencies(types[i], &seen)
		if newUnregisteredTypes != nil {
			unregisteredTypes = append(unregisteredTypes, newUnregisteredTypes...)
		}
	}

	// Resolve the name for every unregistered type.
	typeNames := make([]string, len(unregisteredTypes))
	var err error
	for i := range unregisteredTypes {
		typeNames[i], err = e.getTypeName(ctx, unregisteredTypes[i])
		if err != nil {
			return err
		}
	}

	return fmt.Errorf("%s: %s", message, strings.Join(typeNames, ", "))
}
