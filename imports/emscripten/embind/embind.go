package embind

import (
	embind_internal "github.com/tetratelabs/wazero/internal/emscripten/embind"
)

type Engine interface {
	embind_internal.Engine
}

type Enum interface {
	embind_internal.Enum
}

type EngineKey = embind_internal.EngineKey

func CreateEngine() Engine {
	return embind_internal.CreateEngine()
}

type EmvalConstructor interface {
	embind_internal.EmvalConstructor
}

type EmvalFunctionMapper interface {
	embind_internal.EmvalFunctionMapper
}

type EmvalClassBase struct {
	embind_internal.EmvalClassBase
}
