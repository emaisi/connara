package workflow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"apihub-go/internal/jsonutil"
)

// Resolve renders a template value against the run context: {{path}}
// placeholders are replaced recursively through maps and slices. A string
// that is exactly one placeholder keeps the referenced JSON type (including
// json.Number); mixed text only accepts string, number and boolean parts.
func Resolve(context map[string]any, value any) (any, error) {
	return resolveValue(context, value, false)
}

// ResolveOutput additionally accepts {{?path}} optional references, which
// yield null when the path is missing. They are reserved for the
// workflow-level output mapping where conditional branches may skip fields.
func ResolveOutput(context map[string]any, value any) (any, error) {
	return resolveValue(context, value, true)
}

func resolveValue(context map[string]any, value any, allowOptional bool) (any, error) {
	switch typed := value.(type) {
	case string:
		return resolveString(context, typed, allowOptional)
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			resolved, err := resolveValue(context, item, allowOptional)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			resolved, err := resolveValue(context, item, allowOptional)
			if err != nil {
				return nil, err
			}
			result[index] = resolved
		}
		return result, nil
	default:
		return value, nil
	}
}

type placeholder struct {
	path     string
	optional bool
}

func resolveString(context map[string]any, text string, allowOptional bool) (any, error) {
	whole, ok := exactPlaceholder(text)
	if ok {
		if whole.optional && !allowOptional {
			return nil, fmt.Errorf("optional reference {{?%s}} is only allowed in the workflow output mapping", whole.path)
		}
		value, found := jsonutil.PathLookup(context, whole.path)
		if !found {
			if whole.optional {
				return nil, nil
			}
			return nil, fmt.Errorf("template path {{%s}} was not found", whole.path)
		}
		return value, nil
	}
	var result strings.Builder
	remaining := text
	for {
		start := strings.Index(remaining, "{{")
		if start < 0 {
			result.WriteString(remaining)
			return result.String(), nil
		}
		result.WriteString(remaining[:start])
		end := strings.Index(remaining[start+2:], "}}")
		if end < 0 {
			return nil, fmt.Errorf("template text %q has an unterminated placeholder", text)
		}
		expression := remaining[start+2 : start+2+end]
		if expression == "" || strings.ContainsAny(expression, "{}?") {
			return nil, fmt.Errorf("template placeholder {{%s}} is malformed", expression)
		}
		if _, valid := jsonutil.PathSegments(expression); !valid {
			return nil, fmt.Errorf("template placeholder {{%s}} is malformed", expression)
		}
		value, found := jsonutil.PathLookup(context, expression)
		if !found {
			return nil, fmt.Errorf("template path {{%s}} was not found", expression)
		}
		part, err := interpolate(value, expression)
		if err != nil {
			return nil, err
		}
		result.WriteString(part)
		remaining = remaining[start+2+end+2:]
	}
}

// exactPlaceholder reports whether text is exactly one {{...}} placeholder.
func exactPlaceholder(text string) (placeholder, bool) {
	if !strings.HasPrefix(text, "{{") || !strings.HasSuffix(text, "}}") || len(text) < 5 {
		return placeholder{}, false
	}
	expression := text[2 : len(text)-2]
	optional := false
	if strings.HasPrefix(expression, "?") {
		optional = true
		expression = expression[1:]
	}
	if expression == "" {
		return placeholder{}, false
	}
	if _, valid := jsonutil.PathSegments(expression); !valid {
		return placeholder{}, false
	}
	return placeholder{path: expression, optional: optional}, true
}

func interpolate(value any, path string) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case bool:
		return strconv.FormatBool(typed), nil
	case json.Number:
		return typed.String(), nil
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	default:
		return "", fmt.Errorf("template path {{%s}} resolves to %s, which cannot be interpolated into mixed text", path, kindOf(value))
	}
}

func kindOf(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	default:
		return fmt.Sprintf("a %T", value)
	}
}
