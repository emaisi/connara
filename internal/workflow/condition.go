package workflow

import (
	"encoding/json"
	"fmt"
	"strconv"

	"apihub-go/internal/jsonutil"
)

// Condition is a restricted assertion: either a leaf comparison or an
// all/any group. It is data, never a script.
type Condition struct {
	Path  string       `json:"path,omitempty"`
	Op    string       `json:"op,omitempty"`
	Value any          `json:"value,omitempty"`
	All   []*Condition `json:"all,omitempty"`
	Any   []*Condition `json:"any,omitempty"`
}

var conditionOps = map[string]bool{
	"eq": true, "ne": true, "gt": true, "gte": true,
	"lt": true, "lte": true, "exists": true, "not_exists": true,
}

// ValidateCondition checks the leaf operator whitelist, the all/any nesting
// depth (at most 2 levels), the leaf budget (at most 8) and path syntax.
func ValidateCondition(condition *Condition) error {
	return validateCondition(condition, 1, newLeafBudget(MaxStepOnRunIf))
}

type leafBudget struct{ remaining int }

func newLeafBudget(total int) *leafBudget { return &leafBudget{remaining: total} }

func validateCondition(condition *Condition, depth int, budget *leafBudget) error {
	if condition == nil {
		return nil
	}
	groups := 0
	if condition.All != nil {
		groups++
	}
	if condition.Any != nil {
		groups++
	}
	if groups > 0 {
		if condition.Path != "" || condition.Op != "" {
			return fmt.Errorf("condition group must not mix path/op with all/any")
		}
		if groups > 1 || len(condition.All) == 0 && len(condition.Any) == 0 {
			return fmt.Errorf("condition group must contain exactly one non-empty all or any list")
		}
		if depth > 2 {
			return fmt.Errorf("condition nesting exceeds two levels")
		}
		var items []*Condition
		if len(condition.All) > 0 {
			items = condition.All
		} else {
			items = condition.Any
		}
		for _, item := range items {
			if item == nil {
				return fmt.Errorf("condition group contains an empty entry")
			}
			if err := validateCondition(item, depth+1, budget); err != nil {
				return err
			}
		}
		return nil
	}
	if !conditionOps[condition.Op] {
		return fmt.Errorf("unsupported condition operator %q", condition.Op)
	}
	if _, ok := jsonutil.PathSegments(condition.Path); !ok {
		return fmt.Errorf("condition path %q is invalid", condition.Path)
	}
	if isOrderingOp(condition.Op) {
		switch condition.Value.(type) {
		case json.Number, float64, int, int64:
		default:
			return fmt.Errorf("operator %q requires a numeric value", condition.Op)
		}
	}
	budget.remaining--
	if budget.remaining < 0 {
		return fmt.Errorf("condition exceeds %d leaf assertions", MaxStepOnRunIf)
	}
	return nil
}

func isOrderingOp(op string) bool {
	return op == "gt" || op == "gte" || op == "lt" || op == "lte"
}

// Evaluate resolves a condition against the run context. all/any groups
// short-circuit in order; missing paths are errors except for exists and
// not_exists, and a present null value counts as existing.
func Evaluate(context map[string]any, condition *Condition) (bool, error) {
	if condition == nil {
		return true, nil
	}
	if len(condition.All) > 0 {
		for _, item := range condition.All {
			matches, err := Evaluate(context, item)
			if err != nil {
				return false, err
			}
			if !matches {
				return false, nil
			}
		}
		return true, nil
	}
	if len(condition.Any) > 0 {
		for _, item := range condition.Any {
			matches, err := Evaluate(context, item)
			if err != nil {
				return false, err
			}
			if matches {
				return true, nil
			}
		}
		return false, nil
	}
	value, found := jsonutil.PathLookup(context, condition.Path)
	switch condition.Op {
	case "exists":
		return found, nil
	case "not_exists":
		return !found, nil
	}
	if !found {
		return false, fmt.Errorf("condition path %q was not found", condition.Path)
	}
	switch condition.Op {
	case "eq":
		return deepEqual(normalizeJSON(condition.Value), normalizeJSON(value)), nil
	case "ne":
		return !deepEqual(normalizeJSON(condition.Value), normalizeJSON(value)), nil
	case "gt", "gte", "lt", "lte":
		subject, subjectErr := numeric(value)
		target, targetErr := numeric(condition.Value)
		if subjectErr != nil || targetErr != nil {
			return false, fmt.Errorf("operator %q requires numbers on both sides", condition.Op)
		}
		switch condition.Op {
		case "gt":
			return subject > target, nil
		case "gte":
			return subject >= target, nil
		case "lt":
			return subject < target, nil
		default:
			return subject <= target, nil
		}
	default:
		return false, fmt.Errorf("unsupported condition operator %q", condition.Op)
	}
}

func numeric(value any) (float64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Float64()
	case float64:
		return typed, nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	default:
		return 0, fmt.Errorf("value is not a number")
	}
}

// normalizeJSON canonicalizes decoded JSON so 1, 1.0 and "1" as json.Number
// compare by value: integers keep full 64-bit precision, others fall back to
// float64.
func normalizeJSON(value any) any {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := strconv.ParseInt(typed.String(), 10, 64); err == nil {
			return integer
		}
		if floating, err := typed.Float64(); err == nil {
			return floating
		}
		return typed.String()
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = normalizeJSON(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = normalizeJSON(item)
		}
		return result
	default:
		return value
	}
}

func deepEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftJSON) == string(rightJSON)
}
