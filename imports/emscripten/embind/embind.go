package embind

import (
	embind_internal "github.com/tetratelabs/wazero/internal/emscripten/embind"
)

type EmbindEngine interface {
	embind_internal.Engine
}

type EngineKey = embind_internal.EngineKey

func CreateEngine() EmbindEngine {
	return embind_internal.CreateEngine()
}
