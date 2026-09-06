package provider

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	"google.golang.org/protobuf/types/known/structpb"
)

// celEnv is the one environment every mapping expression compiles in. Two
// variables are declared: self, the whole document, and item, the current
// device object. Both are dynamic because the document shape is unknown.
var celEnv = func() *cel.Env {
	env, err := cel.NewEnv(
		cel.Variable("self", cel.DynType),
		cel.Variable("item", cel.DynType),
		cel.OptionalTypes(),
		ext.Strings(),
		ext.Math(),
		ext.Lists(),
		ext.Sets(),
		ext.Regex(),
		ext.Bindings(),
	)
	if err != nil {
		panic(err)
	}
	return env
}()

// programCache holds compiled programs by expression text. Expressions are
// small and repeat across runs, so compiling each once is worth the memory.
var programCache sync.Map

// CompileExpression compiles a mapping or pagination expression. It is
// exported so the admission webhook can reject a TargetSource whose
// expressions do not compile instead of accepting one that quietly produces
// fewer targets.
func CompileExpression(expr string) (cel.Program, error) {
	if cached, ok := programCache.Load(expr); ok {
		return cached.(cel.Program), nil
	}
	ast, issues := celEnv.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	prog, err := celEnv.Program(ast, cel.EvalOptions(cel.OptOptimize))
	if err != nil {
		return nil, err
	}
	programCache.Store(expr, prog)
	return prog, nil
}

// evalExpression runs a program against a document and an item and returns
// the result as plain Go values.
func evalExpression(p cel.Program, self, item any) (any, error) {
	out, _, err := p.Eval(map[string]any{"self": self, "item": item})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, nil
	}
	return normalizeCEL(out.Value()), nil
}

// normalizeCEL converts CEL result values into the types the rest of the
// package works with: []any, map[string]any, and scalars.
func normalizeCEL(v any) any {
	switch raw := v.(type) {
	case nil, structpb.NullValue:
		// CEL's null surfaces as a protobuf NullValue; callers test for nil.
		return nil
	case ref.Val:
		inner := raw.Value()
		if inner == nil {
			return nil
		}
		return normalizeCEL(inner)
	case []any:
		out := make([]any, len(raw))
		for i := range raw {
			out[i] = normalizeCEL(raw[i])
		}
		return out
	case []ref.Val:
		out := make([]any, len(raw))
		for i := range raw {
			out[i] = normalizeCEL(raw[i])
		}
		return out
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Map {
		out := make(map[string]any, rv.Len())
		for _, key := range rv.MapKeys() {
			k := fmt.Sprintf("%v", normalizeCEL(key.Interface()))
			out[k] = normalizeCEL(rv.MapIndex(key).Interface())
		}
		return out
	}
	if rv.Kind() == reflect.Slice {
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = normalizeCEL(rv.Index(i).Interface())
		}
		return out
	}
	return v
}
