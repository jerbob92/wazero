package emscripten

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

type cppException struct {
	excPtr  int32
	name    string
	message string
}

func (ce *cppException) Error() string {
	if ce.message == "" {
		return ce.name
	}
	return fmt.Sprintf("%s: %s", ce.name, ce.message)
}

func (ce *cppException) Is(target error) bool {
	_, ok := target.(*cppException)
	return ok
}

func readCString(mod api.Module, addr uint32) (string, error) {
	var sb strings.Builder
	for {
		b, success := mod.Memory().ReadByte(addr)
		if !success {
			return "", errors.New("could not read CString data")
		}

		// Stop when we encounter null terminator of Cstring
		if b == 0 {
			break
		}

		// Write byte to stringbuilder
		sb.WriteByte(b)

		// Move to next char
		addr++
	}

	return sb.String(), nil
}

func newCppException(ctx context.Context, mod api.Module, excPtr int32) (*cppException, error) {
	exception := &cppException{
		excPtr: excPtr,
	}

	// Get the exception type and message if we have the tools to do so.
	if mod.ExportedFunction("malloc") != nil && mod.ExportedFunction("free") != nil && mod.ExportedFunction("__get_exception_message") != nil {
		name, message, err := func(ptr int32) (string, string, error) {
			savedStack, err := mod.ExportedFunction("stackSave").Call(ctx)
			if err != nil {
				return "", "", err
			}

			defer func() {
				_, err := mod.ExportedFunction("stackRestore").Call(ctx, savedStack[0])
				if err != nil {
					panic(err)
				}
			}()

			// Allocate 8 bytes, 4 for the address to the type, 4 for the address
			// to the message.
			exceptionAddressesRes, err := mod.ExportedFunction("malloc").Call(ctx, 8)
			if err != nil {
				return "", "", err
			}

			// Let Emscripten allocate the strings for the type and message and set
			// the addresses to the allocated bytes.
			_, err = mod.ExportedFunction("__get_exception_message").Call(ctx, api.EncodeI32(excPtr), exceptionAddressesRes[0], exceptionAddressesRes[0]+4)
			if err != nil {
				return "", "", err
			}

			// Read the address to the type string.
			typeAddr, ok := mod.Memory().ReadUint32Le(uint32(exceptionAddressesRes[0]))
			if !ok {
				return "", "", errors.New("could not read typeAddr from memory")
			}

			// Read the type string from memory and free the memory.
			typeString, err := readCString(mod, typeAddr)
			mod.ExportedFunction("free").Call(ctx, api.EncodeU32(typeAddr))
			if err != nil {
				return "", "", err
			}

			// Read the address to the message string.
			messageAddr, ok := mod.Memory().ReadUint32Le(uint32(exceptionAddressesRes[0] + 4))
			if !ok {
				return "", "", errors.New("could not read messageAddr from memory")
			}

			// Read the message string from memory and free the memory if it has
			// been set. The message is only there in some cases.
			messageString := ""
			if messageAddr > 0 {
				messageString, err = readCString(mod, messageAddr)
				mod.ExportedFunction("free").Call(ctx, api.EncodeU32(messageAddr))
				if err != nil {
					return "", "", err
				}
			}

			return typeString, messageString, nil
		}(excPtr)
		if err != nil {
			return nil, err
		}

		exception.name = name
		exception.message = message
	}

	return exception, nil
}

func newExceptionInfo(excPtr int32) exceptionInfo {
	return exceptionInfo{
		excPtr: excPtr,
		ptr:    excPtr - 24,
	}
}

type exceptionInfo struct {
	excPtr int32
	ptr    int32
}

func (ei *exceptionInfo) SetType(mod api.Module, exceptionType int32) {
	mod.Memory().WriteUint32Le(uint32(ei.ptr+4), uint32(exceptionType))
}

func (ei *exceptionInfo) GetType(mod api.Module) int32 {
	val, _ := mod.Memory().ReadUint32Le(uint32(ei.ptr + 4))
	return int32(val)
}

func (ei *exceptionInfo) SetDestructor(mod api.Module, destructor int32) {
	mod.Memory().WriteUint32Le(uint32(ei.ptr+8), uint32(destructor))
}

func (ei *exceptionInfo) GetDestructor(mod api.Module) int32 {
	val, _ := mod.Memory().ReadUint32Le(uint32(ei.ptr + 8))
	return int32(val)
}

func (ei *exceptionInfo) SetCaught(mod api.Module, caught int8) {
	mod.Memory().WriteByte(uint32(ei.ptr+12), byte(caught))
}

func (ei *exceptionInfo) GetCaught(mod api.Module) int8 {
	val, _ := mod.Memory().ReadByte(uint32(ei.ptr + 12))
	return int8(val)
}

func (ei *exceptionInfo) SetRethrown(mod api.Module, rethrown int8) {
	mod.Memory().WriteByte(uint32(ei.ptr+13), byte(rethrown))
}

func (ei *exceptionInfo) GetRethrown(mod api.Module) int8 {
	val, _ := mod.Memory().ReadByte(uint32(ei.ptr + 13))
	return int8(val)
}

// Init initializes native structure fields. Should be called once after allocated.
func (ei *exceptionInfo) Init(mod api.Module, exceptionType, destructor int32) {
	ei.SetAdjustedPtr(mod, 0)
	ei.SetType(mod, exceptionType)
	ei.SetDestructor(mod, destructor)
}

func (ei *exceptionInfo) SetAdjustedPtr(mod api.Module, adjustedPtr int32) {
	mod.Memory().WriteUint32Le(uint32(ei.ptr+16), uint32(adjustedPtr))
}

func (ei *exceptionInfo) GetAdjustedPtr(mod api.Module) int32 {
	val, _ := mod.Memory().ReadUint32Le(uint32(ei.ptr + 16))
	return int32(val)
}

// GetExceptionPtr Get pointer which is expected to be received by catch clause
// in C++ code. It may be adjusted when the pointer is casted to some of the
// exception object base classes (e.g. when virtual  inheritance is used). When
// a pointer is thrown this method should return the thrown pointer itself.
func (ei *exceptionInfo) GetExceptionPtr(ctx context.Context, mod api.Module) (int32, error) {
	// Work around a fastcomp bug, this code is still included for some reason in a build without
	// exceptions support.
	isPointerRes, err := mod.ExportedFunction("__cxa_is_pointer_type").Call(ctx, api.EncodeI32(ei.GetType(mod)))
	if err != nil {
		return 0, err
	}

	if isPointerRes[0] != 0 {
		val, _ := mod.Memory().ReadUint32Le(uint32(ei.excPtr))
		return int32(val), nil
	}

	adjusted := ei.GetAdjustedPtr(mod)
	if adjusted != 0 {
		return adjusted, nil
	}

	return ei.excPtr, nil
}

var uncaughtExceptionCount = int32(0)
var exceptionLast *cppException

const FunctionCxaThrow = "__cxa_throw"

var CxaThrow = &wasm.HostFunc{
	ExportName: FunctionCxaThrow,
	Name:       FunctionCxaThrow,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames: []string{"ptr", "type", "destructor"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, params []uint64) {
		mod = resolveMainModule(ctx, mod)
		ptr := api.DecodeI32(params[0])
		exceptionType := api.DecodeI32(params[1])
		destructor := api.DecodeI32(params[2])

		info := newExceptionInfo(ptr)

		// Initialize ExceptionInfo content after it was allocated in __cxa_allocate_exception.
		info.Init(mod, exceptionType, destructor)
		createdCppException, err := newCppException(ctx, mod, ptr)
		if err != nil {
			panic(err)
		}

		exceptionLast = createdCppException
		uncaughtExceptionCount++
		panic(exceptionLast)
	})},
}

// FindMatchingCatchPrefix is the naming convention of Emscripten dynamic
// exception type matching functions.
//
// Every argument in the function call is a pointer to a C++ type that the
// catch block support. The implementation loops through all arguments
// to check if one of the arguments is of the same type as the exception.
const FindMatchingCatchPrefix = "__cxa_find_matching_catch_"

func NewFindMatchingCatchFunc(importName string, params, results []api.ValueType) *wasm.HostFunc {
	fn := &FindMatchingCatchFunc{&wasm.FunctionType{Results: results, Params: params}}

	// Make friendly parameter names.
	paramNames := make([]string, len(params))
	for i := 0; i < len(paramNames); i++ {
		paramNames[i] = "type" + strconv.Itoa(i)
	}

	return &wasm.HostFunc{
		ExportName:  importName,
		ParamTypes:  params,
		ParamNames:  paramNames,
		ResultTypes: results,
		Code:        wasm.Code{GoFunc: fn},
	}
}

type FindMatchingCatchFunc struct {
	*wasm.FunctionType
}

func (v *FindMatchingCatchFunc) Call(ctx context.Context, mod api.Module, stack []uint64) {
	mod = resolveMainModule(ctx, mod)
	passThroughNull := func() {
		_, err := mod.ExportedFunction("setTempRet0").Call(ctx, 0)
		if err != nil {
			panic(err)
		}
		stack[0] = 0
	}

	if exceptionLast == nil {
		// just pass through the null ptr
		passThroughNull()
		return
	}

	thrown := exceptionLast.excPtr

	info := newExceptionInfo(thrown)
	info.SetAdjustedPtr(mod, thrown)

	thrownType := info.GetType(mod)
	if thrownType == 0 {
		// just pass through the null ptr
		passThroughNull()
		return
	}

	// can_catch receives a **, add indirection
	// The different catch blocks are denoted by different types.
	// Due to inheritance, those types may not precisely match the
	// type of the thrown object. Find one which matches, and
	// return the type of the catch block which should be called.
	for i := range stack {
		caughtType := api.DecodeI32(stack[i])
		if caughtType == 0 || caughtType == thrownType {
			// Catch all clause matched or exactly the same type is caught
			break
		}

		adjustedPtrAddr := info.ptr + 16
		canCatchRes, err := mod.ExportedFunction("__cxa_can_catch").Call(ctx, api.EncodeI32(caughtType), api.EncodeI32(thrownType), api.EncodeI32(adjustedPtrAddr))
		if err != nil {
			panic(err)
		}

		if canCatchRes[0] > 0 {
			_, err := mod.ExportedFunction("setTempRet0").Call(ctx, api.EncodeI32(caughtType))
			if err != nil {
				panic(err)
			}
			stack[0] = api.EncodeI32(thrown)
		}
	}

	_, err := mod.ExportedFunction("setTempRet0").Call(ctx, api.EncodeI32(thrownType))
	if err != nil {
		panic(err)
	}
	stack[0] = api.EncodeI32(thrown)
}

const FunctionLlvmEhTypeidFor = "llvm_eh_typeid_for"

var LlvmEhTypeidFor = &wasm.HostFunc{
	ExportName:  FunctionLlvmEhTypeidFor,
	Name:        FunctionLlvmEhTypeidFor,
	ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"type"},
	ResultTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"type"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(context.Context, api.Module, []uint64) {
		// This function doesn't seem to do anything.
		// JS:
		// function _llvm_eh_typeid_for(type) {
		//   return type;
		// }
		// We don't have to return type here because it already re-uses the
		// stack anyway so leaving this empty is basically the same as the JS
		// implementation.
	})},
}

const FunctionCxaBeginCatch = "__cxa_begin_catch"

var exceptionCaught = []exceptionInfo{}

var CxaBeginCatch = &wasm.HostFunc{
	ExportName:  FunctionCxaBeginCatch,
	Name:        FunctionCxaBeginCatch,
	ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"ptr"},
	ResultTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"exception_ptr"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		mod = resolveMainModule(ctx, mod)
		info := newExceptionInfo(api.DecodeI32(stack[0]))
		if info.GetCaught(mod) == 0 {
			info.SetCaught(mod, 1)
			uncaughtExceptionCount--
		}
		info.SetRethrown(mod, 0)
		exceptionCaught = append(exceptionCaught, info)

		_, err := mod.ExportedFunction("__cxa_increment_exception_refcount").Call(ctx, api.EncodeI32(info.excPtr))
		if err != nil {
			panic(err)
		}

		exceptionPtr, err := info.GetExceptionPtr(ctx, mod)
		if err != nil {
			panic(err)
		}
		stack[0] = api.EncodeI32(exceptionPtr)
	})},
}

const FunctionCxaEndCatch = "__cxa_end_catch"

var CxaEndCatch = &wasm.HostFunc{
	ExportName: FunctionCxaEndCatch,
	Name:       FunctionCxaEndCatch,
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		mod = resolveMainModule(ctx, mod)
		_, err := mod.ExportedFunction("setThrew").Call(ctx, 0, 0)
		if err != nil {
			panic(err)
		}

		// Call destructor if one is registered then clear it.
		info := exceptionCaught[len(exceptionCaught)-1]
		exceptionCaught = exceptionCaught[:len(exceptionCaught)-1]

		_, err = mod.ExportedFunction("__cxa_decrement_exception_refcount").Call(ctx, api.EncodeI32(info.excPtr))
		if err != nil {
			panic(err)
		}

		exceptionLast = nil
	})},
}

const FunctionResumeException = "__resumeException"

var ResumeException = &wasm.HostFunc{
	ExportName: FunctionResumeException,
	Name:       FunctionResumeException,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames: []string{"ptr"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		if exceptionLast == nil {
			mod = resolveMainModule(ctx, mod)
			exception, err := newCppException(ctx, mod, api.DecodeI32(stack[0]))
			if err != nil {
				panic(err)
			}
			exceptionLast = exception
		}
		panic(exceptionLast)
	})},
}

const FunctionCxaRethrow = "__cxa_rethrow"

var CxaRethrow = &wasm.HostFunc{
	ExportName: FunctionCxaRethrow,
	Name:       FunctionCxaRethrow,
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		if len(exceptionCaught) == 0 {
			panic("no exception to throw")
		}
		mod = resolveMainModule(ctx, mod)

		// Get the last entry and pop it from the list.
		info := exceptionCaught[len(exceptionCaught)-1]
		exceptionCaught = exceptionCaught[:len(exceptionCaught)-1]

		ptr := info.excPtr
		if info.GetRethrown(mod) == 0 {
			// Only pop if the corresponding push was through rethrow_primary_exception
			exceptionCaught = append(exceptionCaught, info)
			info.SetRethrown(mod, 1)
			info.SetCaught(mod, 0)
			uncaughtExceptionCount++
		}
		exception, err := newCppException(ctx, mod, ptr)
		if err != nil {
			panic(err)
		}
		exceptionLast = exception
		panic(exceptionLast)
	})},
}

const FunctionCxaUncaughtExceptions = "__cxa_uncaught_exceptions"

var CxaUncaughtExceptions = &wasm.HostFunc{
	ExportName:  FunctionCxaUncaughtExceptions,
	Name:        FunctionCxaUncaughtExceptions,
	ResultTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"count"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = api.EncodeI32(uncaughtExceptionCount)
	})},
}

const FunctionCxaGetExceptionPtr = "__cxa_get_exception_ptr"

var CxaGetExceptionPtr = &wasm.HostFunc{
	ExportName:  FunctionCxaGetExceptionPtr,
	Name:        FunctionCxaGetExceptionPtr,
	ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"ptr"},
	ResultTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"exception_ptr"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		mod = resolveMainModule(ctx, mod)
		info := newExceptionInfo(api.DecodeI32(stack[0]))
		rtn, err := info.GetExceptionPtr(ctx, mod)
		if err != nil {
			panic(err)
		}
		stack[0] = api.EncodeI32(rtn)
	})},
}

const FunctionCxaCallUnexpected = "__cxa_call_unexpected"

var CxaCallUnexpected = &wasm.HostFunc{
	ExportName: FunctionCxaCallUnexpected,
	Name:       FunctionCxaCallUnexpected,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames: []string{"exception"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		panic(errors.New("Unexpected exception thrown, this is not properly supported - aborting"))
	})},
}

const FunctionCxaCurrentPrimaryException = "__cxa_current_primary_exception"

var CxaCurrentPrimaryException = &wasm.HostFunc{
	ExportName:  FunctionCxaCurrentPrimaryException,
	Name:        FunctionCxaCurrentPrimaryException,
	ResultTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"ptr"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		if len(exceptionCaught) == 0 {
			stack[0] = 0
			return
		}
		mod = resolveMainModule(ctx, mod)
		info := exceptionCaught[len(exceptionCaught)-1]
		_, err := mod.ExportedFunction("__cxa_increment_exception_refcount").Call(ctx, api.EncodeI32(info.excPtr))
		if err != nil {
			panic(err)
		}
		stack[0] = api.EncodeI32(info.excPtr)
		return
	})},
}

const FunctionCxaRethrowPrimaryException = "__cxa_rethrow_primary_exception"

var CxaRethrowPrimaryException = &wasm.HostFunc{
	ExportName: FunctionCxaRethrowPrimaryException,
	Name:       FunctionCxaRethrowPrimaryException,
	ParamTypes: []wasm.ValueType{wasm.ValueTypeI32},
	ParamNames: []string{"ptr"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		mod = resolveMainModule(ctx, mod)
		if stack[0] == 0 {
			return
		}
		info := newExceptionInfo(api.DecodeI32(stack[0]))
		exceptionCaught = append(exceptionCaught, info)
		info.SetRethrown(mod, 1)

		_, err := mod.ExportedFunction("__cxa_rethrow").Call(ctx, api.EncodeI32(info.excPtr))
		if err != nil {
			panic(err)
		}
	})},
}
