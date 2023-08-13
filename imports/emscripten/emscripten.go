// Package emscripten contains Go-defined special functions imported by
// Emscripten under the module name "env".
//
// Emscripten has many imports which are triggered on build flags. Use
// FunctionExporter, instead of Instantiate, to define more "env" functions.
//
// # Relationship to WASI
//
// Emscripten typically requires wasi_snapshot_preview1 to implement exit.
//
// See wasi_snapshot_preview1.Instantiate and
// https://github.com/emscripten-core/emscripten/wiki/WebAssembly-Standalone
package emscripten

import (
	"context"
	"strings"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	internal "github.com/tetratelabs/wazero/internal/emscripten"
	embind_internal "github.com/tetratelabs/wazero/internal/emscripten/embind"
	"github.com/tetratelabs/wazero/internal/wasm"
)

const i32 = wasm.ValueTypeI32

// MustInstantiate calls Instantiate or panics on error.
//
// This is a simpler function for those who know the module "env" is not
// already instantiated, and don't need to unload it.
func MustInstantiate(ctx context.Context, r wazero.Runtime) {
	if _, err := Instantiate(ctx, r); err != nil {
		panic(err)
	}
}

// Instantiate instantiates the "env" module used by Emscripten into the
// runtime.
//
// # Notes
//
//   - Failure cases are documented on wazero.Runtime InstantiateModule.
//   - Closing the wazero.Runtime has the same effect as closing the result.
//   - To add more functions to the "env" module, use FunctionExporter.
func Instantiate(ctx context.Context, r wazero.Runtime) (api.Closer, error) {
	builder := r.NewHostModuleBuilder("env")
	NewFunctionExporter().ExportFunctions(builder)
	return builder.Instantiate(ctx)
}

// FunctionExporter configures the functions in the "env" module used by
// Emscripten.
//
// # Notes
//
//   - This is an interface for decoupling, not third-party implementations.
//     All implementations are in wazero.
type FunctionExporter interface {
	// ExportFunctions builds functions to export with a wazero.HostModuleBuilder
	// named "env".
	ExportFunctions(wazero.HostModuleBuilder)
}

// NewFunctionExporter returns a FunctionExporter object with trace disabled.
func NewFunctionExporter() FunctionExporter {
	return &functionExporter{}
}

type functionExporter struct{}

// ExportFunctions implements FunctionExporter.ExportFunctions
func (functionExporter) ExportFunctions(builder wazero.HostModuleBuilder) {
	exporter := builder.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(internal.NotifyMemoryGrowth)
}

type emscriptenFns []*wasm.HostFunc

// InstantiateForModule instantiates a module named "env" populated with any
// known functions used in emscripten.
func InstantiateForModule(ctx context.Context, r wazero.Runtime, guest wazero.CompiledModule) (api.Closer, error) {
	// Create the exporter for the supplied wasm
	exporter, err := NewFunctionExporterForModule(guest)
	if err != nil {
		return nil, err
	}

	// Instantiate it!
	env := r.NewHostModuleBuilder("env")
	exporter.ExportFunctions(env)
	return env.Instantiate(ctx)
}

// NewFunctionExporterForModule returns a guest-specific FunctionExporter,
// populated with any known functions used in emscripten.
func NewFunctionExporterForModule(guest wazero.CompiledModule) (FunctionExporter, error) {
	functionMap := map[string]*wasm.HostFunc{
		internal.FunctionNotifyMemoryGrowth:              internal.NotifyMemoryGrowth,
		internal.FunctionThrowLongjmp:                    internal.ThrowLongjmp,
		embind_internal.FunctionEmbindRegisterVoid:       embind_internal.EmbindRegisterVoid,
		embind_internal.FunctionEmbindRegisterFunction:   embind_internal.EmbindRegisterFunction,
		embind_internal.FunctionEmbindRegisterBool:       embind_internal.EmbindRegisterBool,
		embind_internal.FunctionEmbindRegisterInteger:    embind_internal.EmbindRegisterInteger,
		embind_internal.FunctionEmbindRegisterBigInt:     embind_internal.EmbindRegisterBigInt,
		embind_internal.FunctionEmbindRegisterFloat:      embind_internal.EmbindRegisterFloat,
		embind_internal.FunctionEmbindRegisterStdString:  embind_internal.EmbindRegisterStdString,
		embind_internal.FunctionEmbindRegisterStdWString: embind_internal.EmbindRegisterStdWString,
		embind_internal.FunctionEmbindRegisterEmval:      embind_internal.EmbindRegisterEmval,
		embind_internal.FunctionEmbindRegisterMemoryView: embind_internal.EmbindRegisterMemoryView,
		embind_internal.FunctionEmbindRegisterConstant:   embind_internal.EmbindRegisterConstant,
		embind_internal.FunctionEmbindRegisterEnum:       embind_internal.EmbindRegisterEnum,
		embind_internal.FunctionEmbindRegisterEnumValue:  embind_internal.EmbindRegisterEnumValue,
	}
	ret := emscriptenFns{}
	for _, fn := range guest.ImportedFunctions() {
		importModule, importName, isImport := fn.Import()
		if !isImport || importModule != "env" {
			continue // not emscripten
		}

		hf, ok := functionMap[importName]
		if ok {
			ret = append(ret, hf)
			continue
		}

		if strings.HasPrefix(importName, internal.InvokePrefix) {
			hf = internal.NewInvokeFunc(importName, fn.ParamTypes(), fn.ResultTypes())
			ret = append(ret, hf)
		}

		// not invoke, and maybe not emscripten
	}
	return ret, nil
}

// ExportFunctions implements FunctionExporter.ExportFunctions
func (i emscriptenFns) ExportFunctions(builder wazero.HostModuleBuilder) {
	exporter := builder.(wasm.HostFuncExporter)
	for _, fn := range i {
		exporter.ExportHostFunc(fn)
	}
}
