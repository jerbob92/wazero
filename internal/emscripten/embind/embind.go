package embind

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

type Engine interface {
	CallFunction(ctx context.Context, name string, arguments ...any) (any, error)
}

type EngineKey struct{}

func GetEngineFromContext(ctx context.Context) (Engine, error) {
	raw := ctx.Value(EngineKey{})
	if raw == nil {
		return nil, fmt.Errorf("embind engine not found in context")
	}

	value, ok := raw.(Engine)
	if !ok {
		return nil, fmt.Errorf("context value %v not of type %T", value, new(Engine))
	}

	return value, nil
}

func MustGetEngineFromContext(ctx context.Context, mod api.Module) Engine {
	engine, err := GetEngineFromContext(ctx)
	if err != nil {
		panic(fmt.Errorf("could not get embind engine from context: %w, make sure to create an engine with embind.CreateEngine() and to attach it to the context with \"ctx = context.WithValue(ctx, embind.EngineKey{}, engine)\"", err))
	}

	if engine.(*embindEngine).mod != nil {
		if engine.(*embindEngine).mod != mod {
			panic(fmt.Errorf("could not get embind engine from context, this engine was created for another Wazero api.Module"))
		}
	}

	// Make sure we have the api module set.
	engine.(*embindEngine).mod = mod

	return engine
}

func CreateEngine() Engine {
	return &embindEngine{
		publicSymbols:        map[string]*publicSymbol{},
		registeredTypes:      map[int32]*registeredType{},
		typeDependencies:     map[int32][]int32{},
		awaitingDependencies: map[int32][]*awaitingDependency{},
	}
}
