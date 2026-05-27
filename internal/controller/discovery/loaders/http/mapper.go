package http

import (
	"fmt"
	"math"
	"reflect"
	"strconv"

	"github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
)

// mapItemsToTargets converts a list of raw JSON items into DiscoveredTargets using the configured mapping rules
func (l *Loader) mapItemsToTargets(items []any, full any, logger logr.Logger) ([]core.DiscoveredTarget, error) {
	// Compile CEL expressions once for efficiency
	compiled, err := l.compileMapping()
	if err != nil {
		return nil, fmt.Errorf("compile mapping: %w", err)
	}

	// Map items to targets
	targets := make([]core.DiscoveredTarget, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			logger.Error(fmt.Errorf("invalid target format"),
				"failed to convert target to map",
				"item", item,
			)
			continue
		}
		target, err := l.mapItemToTarget(obj, full, compiled)
		if err != nil {
			logger.Error(err,
				"failed to map target",
				"item", obj,
			)
			continue
		}

		targets = append(targets, target)
	}

	return targets, nil
}

type compiledMapping struct {
	name    cel.Program
	address cel.Program
	port    cel.Program

	targetProfile cel.Program
	labels        cel.Program
}

func (l *Loader) compileMapping() (*compiledMapping, error) {
	rm := l.spec.ResponseMapping
	cm := &compiledMapping{}
	if rm == nil {
		return cm, nil
	}

	var err error
	if rm.Name != "" {
		cm.name, err = compileCEL(rm.Name)
		if err != nil {
			return nil, fmt.Errorf("name: %w", err)
		}
	}
	if rm.Address != "" {
		cm.address, err = compileCEL(rm.Address)
		if err != nil {
			return nil, fmt.Errorf("address: %w", err)
		}
	}
	if rm.Port != "" {
		cm.port, err = compileCEL(rm.Port)
		if err != nil {
			return nil, fmt.Errorf("port: %w", err)
		}
	}
	if rm.TargetProfile != "" {
		cm.targetProfile, err = compileCEL(rm.TargetProfile)
		if err != nil {
			return nil, fmt.Errorf("targetProfile: %w", err)
		}
	}
	if rm.Labels != "" {
		cm.labels, err = compileCEL(rm.Labels)
		if err != nil {
			return nil, fmt.Errorf("labels: %w", err)
		}
	}

	return cm, nil
}

// mapItemToTarget converts a raw JSON object into a DiscoveredTarget
func (l *Loader) mapItemToTarget(item map[string]any, full any, cm *compiledMapping) (core.DiscoveredTarget, error) {
	name, err := l.getName(item, full, cm)
	if err != nil {
		return core.DiscoveredTarget{}, err
	}

	address, err := l.getAddress(item, full, cm)
	if err != nil {
		return core.DiscoveredTarget{}, err
	}

	return core.DiscoveredTarget{
		Name:          name,
		Address:       address,
		Port:          l.getPort(item, full, cm),
		Labels:        l.getLabels(item, full, cm),
		TargetProfile: l.getTargetProfile(item, full, cm),
	}, nil
}

// getName extracts the target name from the item using the compiled CEL expression if provided,
// otherwise it falls back to the default "name" field
func (l *Loader) getName(item map[string]any, full any, cm *compiledMapping) (string, error) {
	if cm.name != nil {
		val, err := evalCEL(cm.name, item, full)
		if err != nil {
			return "", err
		}

		str, ok := val.(string)
		if !ok || str == "" {
			return "", fmt.Errorf("name must be non-empty string")
		}
		return str, nil
	}

	val, ok := item["name"].(string)
	if !ok || val == "" {
		return "", fmt.Errorf("name must be non-empty string")
	}
	return val, nil
}

// getAddress extracts the target address from the item using the compiled CEL expression if provided,
// otherwise it falls back to the default "address" field
func (l *Loader) getAddress(item map[string]any, full any, cm *compiledMapping) (string, error) {
	if cm.address != nil {
		val, err := evalCEL(cm.address, item, full)
		if err != nil {
			return "", err
		}

		str, ok := val.(string)
		if !ok || str == "" {
			return "", fmt.Errorf("address must be non-empty string")
		}
		return str, nil
	}

	val, ok := item["address"].(string)
	if !ok || val == "" {
		return "", fmt.Errorf("address must be non-empty string")
	}
	return val, nil
}

// getPort extracts the target port from the item using the compiled CEL expression if provided,
// otherwise it falls back to the default "port" field
func (l *Loader) getPort(item map[string]any, full any, cm *compiledMapping) int32 {
	if cm.port != nil {
		val, err := evalCEL(cm.port, item, full)
		if err == nil {
			return extractPort(val)
		}
		return 0
	}

	return extractPort(item["port"])
}

// getLabels extracts the target labels from the item using the compiled CEL expressions if provided,
// otherwise it falls back to the default "labels" field
func (l *Loader) getLabels(item map[string]any, full any, cm *compiledMapping) map[string]string {
	result := make(map[string]string)

	if cm != nil && cm.labels != nil {
		val, err := evalCEL(cm.labels, item, full)
		if err != nil {
			return result
		}
		m, ok := val.(map[string]any)
		if !ok {
			return result
		}
		for k, v := range m {
			result[k] = fmt.Sprintf("%v", v)
		}
	}

	// fallback: direct
	if raw, ok := item["labels"].(map[string]any); ok {
		for key, val := range raw {
			result[key] = fmt.Sprintf("%v", val)
		}
	}
	return result
}

// getTargetProfile extracts the target profile from the item using the compiled CEL expression if provided,
// otherwise it falls back to the default "targetProfile" field
func (l *Loader) getTargetProfile(item map[string]any, full any, cm *compiledMapping) string {
	if cm.targetProfile != nil {
		val, err := evalCEL(cm.targetProfile, item, full)
		if err == nil {
			if str, ok := val.(string); ok {
				return str
			}
		}
		return ""
	}

	if val, ok := item["targetProfile"].(string); ok {
		return val
	}
	return ""
}

var celEnv = mustNewEnv()

// mustNewEnv creates a CEL environment with the necessary variable declarations for evaluating expressions
func mustNewEnv() *cel.Env {
	env, err := cel.NewEnv(
		cel.Variable("self", cel.DynType),
		cel.Variable("item", cel.DynType),
		// Required for ext.Regex
		cel.OptionalTypes(),
		// TODO: document what extensions are included
		// Include standard CEL declarations for common operations and types
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
}

// compileCEL compiles a CEL expression into a program that can be evaluated against items
func compileCEL(expr string) (cel.Program, error) {
	ast, issues := celEnv.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return celEnv.Program(ast, cel.EvalOptions(cel.OptOptimize))
}

// evalCEL evaluates a compiled CEL program against an item
func evalCEL(p cel.Program, item map[string]any, full any) (any, error) {
	out, _, err := p.Eval(map[string]any{
		"self": full,
		"item": item,
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("CEL returned nil")
	}

	return normalizeCEL(out.Value()), nil
}

// normalizeCEL recursively converts CEL evaluation results into standard Go types
func normalizeCEL(v any) any {
	switch raw := v.(type) {
	case ref.Val:
		v := raw.Value()
		if v == nil {
			return nil
		}
		return normalizeCEL(v)

	case []any:
		for i := range raw {
			raw[i] = normalizeCEL(raw[i])
		}
		return raw
	}

	// For maps, keys are converted to strings
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Map {
		out := make(map[string]any)
		for _, key := range rv.MapKeys() {
			k := fmt.Sprintf("%v", normalizeCEL(key.Interface()))
			val := normalizeCEL(rv.MapIndex(key).Interface())
			out[k] = val
		}
		return out
	}

	return v
}

// extractPort converts a CEL evaluation result into an int32 port number,
// handling both numeric and string representations
func extractPort(val any) int32 {
	switch v := val.(type) {
	case float64:
		if v < 0 || v > math.MaxInt32 {
			return 0
		}
		return int32(v)

	case string:
		p, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return 0
		}
		return int32(p)

	default:
		return 0
	}
}
