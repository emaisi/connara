package workflow

import (
	"apihub-go/internal/jsonutil"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
)

func ValidateV2Condition(condition *Condition) error {
	raw, _ := json.Marshal(condition)
	var copy *Condition
	if err := jsonutil.Unmarshal(raw, &copy); err != nil {
		return err
	}
	var prepare func(*Condition) error
	prepare = func(c *Condition) error {
		if c == nil {
			return nil
		}
		if isOrderingOp(c.Op) {
			refs, err := collectV2References(c.Value)
			if err != nil {
				return err
			}
			marker := false
			if value, ok := c.Value.(map[string]any); ok {
				_, marker, err = valueMarker(value)
				if err != nil {
					return err
				}
			}
			if len(refs) > 0 || marker {
				c.Value = json.Number("0")
			}
		}
		for _, c := range c.All {
			if err := prepare(c); err != nil {
				return err
			}
		}
		for _, c := range c.Any {
			if err := prepare(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := prepare(copy); err != nil {
		return err
	}
	return ValidateCondition(copy)
}

func EvaluateV2(values map[string]any, c *Condition, metadata *RunMetadata) (bool, error) {
	if c == nil {
		return true, nil
	}
	if len(c.All) > 0 {
		for _, child := range c.All {
			ok, err := EvaluateV2(values, child, metadata)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	}
	if len(c.Any) > 0 {
		for _, child := range c.Any {
			ok, err := EvaluateV2(values, child, metadata)
			if err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	}
	leaf := *c
	if c.Op != "exists" && c.Op != "not_exists" {
		value, err := ResolveV2(values, c.Value, metadata, false)
		if err != nil {
			return false, err
		}
		leaf.Value = value
	}
	if leaf.Op == "eq" || leaf.Op == "ne" {
		subject, found := jsonutil.PathLookup(values, leaf.Path)
		if !found {
			return false, fmt.Errorf("condition path %q was not found", leaf.Path)
		}
		equal, err := equalV2(subject, leaf.Value)
		if leaf.Op == "ne" {
			equal = !equal
		}
		return equal, err
	}
	if isOrderingOp(leaf.Op) {
		subject, found := jsonutil.PathLookup(values, leaf.Path)
		if !found {
			return false, fmt.Errorf("condition path %q was not found", leaf.Path)
		}
		a, err := exactNumber(subject)
		if err != nil {
			return false, err
		}
		b, err := exactNumber(leaf.Value)
		if err != nil {
			return false, err
		}
		compared := a.Cmp(b)
		switch leaf.Op {
		case "gt":
			return compared > 0, nil
		case "gte":
			return compared >= 0, nil
		case "lt":
			return compared < 0, nil
		default:
			return compared <= 0, nil
		}
	}
	return Evaluate(values, &leaf)
}

func cloneJSON(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return decodeValue(raw)
}

func checkVariable(variable Variable, value any) error {
	if value == nil {
		if variable.Nullable {
			return nil
		}
		return fmt.Errorf("variable %q does not allow null", variable.Name)
	}
	valid := false
	switch variable.Type {
	case "string":
		_, valid = value.(string)
	case "boolean":
		_, valid = value.(bool)
	case "object":
		_, valid = value.(map[string]any)
	case "array":
		_, valid = value.([]any)
	case "number", "integer":
		if n, ok := value.(json.Number); ok {
			rat, err := exactNumber(n)
			valid = err == nil && (variable.Type == "number" || rat.IsInt())
		}
	}
	if !valid {
		return fmt.Errorf("variable %q has incorrect type", variable.Name)
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaxFinalOutput {
		return fmt.Errorf("variable %q exceeds 4 MiB", variable.Name)
	}
	return nil
}

func InitialVariables(def Definition, trigger map[string]any, metadata *RunMetadata) (map[string]any, error) {
	vars := map[string]any{}
	for index, variable := range def.Variables {
		value, err := decodeValue(variable.Initial)
		if err != nil {
			return nil, Located(err, "variable_initial_invalid", "", "variables", fmt.Sprintf("/graph/variables/%d/initial", index))
		}
		value, err = ResolveV2(map[string]any{"trigger": trigger}, value, metadata, false)
		if err != nil {
			return nil, Located(err, "variable_initial_invalid", "", "variables", fmt.Sprintf("/graph/variables/%d/initial", index))
		}
		value, err = cloneJSON(value)
		if err != nil {
			return nil, Located(err, "variable_initial_invalid", "", "variables", fmt.Sprintf("/graph/variables/%d/initial", index))
		}
		if err := checkVariable(variable, value); err != nil {
			return nil, Located(err, "variable_initial_invalid", "", "variables", fmt.Sprintf("/graph/variables/%d/initial", index))
		}
		vars[variable.Name] = value
	}
	return vars, nil
}

// PrepareAssignments calculates every RHS against the same entry variable
// version. Nothing is visible until the caller commits the complete map.
func PrepareAssignments(declarations []Variable, assignments []Assignment, values map[string]any, metadata *RunMetadata) (map[string]any, error) {
	candidate := map[string]any{}
	byName := map[string]Variable{}
	for _, variable := range declarations {
		byName[variable.Name] = variable
	}
	for index, assignment := range assignments {
		locate := func(err error) error {
			return Located(err, "variable_assignment_invalid", "", "variables", fmt.Sprintf("/assign/%d/value", index))
		}
		declaration, ok := byName[assignment.Variable]
		if !ok {
			return nil, locate(fmt.Errorf("assignment target is undeclared"))
		}
		if _, ok := candidate[assignment.Variable]; ok {
			return nil, locate(fmt.Errorf("duplicate assignment target"))
		}
		value, err := decodeValue(assignment.Value)
		if err != nil {
			return nil, locate(err)
		}
		value, err = ResolveV2(values, value, metadata, false)
		if err != nil {
			return nil, locate(err)
		}
		value, err = cloneJSON(value)
		if err != nil {
			return nil, locate(err)
		}
		if err := checkVariable(declaration, value); err != nil {
			return nil, locate(err)
		}
		candidate[assignment.Variable] = value
	}
	return candidate, nil
}

func validateVariableOrder(def Definition, closures map[string]map[string]bool) error {
	reads := map[string]map[string]bool{}
	writes := map[string]map[string]bool{}
	for _, step := range def.Steps {
		reads[step.ID] = map[string]bool{}
		writes[step.ID] = map[string]bool{}
		values := []any{step.Input, step.Operations}
		if len(step.Source) > 0 {
			value, err := decodeValue(step.Source)
			if err != nil {
				return err
			}
			values = append(values, value)
		}
		for _, ref := range conditionReferences(step.RunIf) {
			values = append(values, "{{"+ref.Path+"}}")
		}
		assignments := append([]Assignment{}, step.Assign...)
		for _, branch := range step.Branches {
			assignments = append(assignments, branch.Assign...)
			for _, ref := range conditionReferences(branch.Condition) {
				values = append(values, "{{"+ref.Path+"}}")
			}
			_ = checkConditionValues(branch.Condition, func(value any) error { values = append(values, value); return nil })
		}
		for _, assignment := range assignments {
			writes[step.ID][assignment.Variable] = true
			value, err := decodeValue(assignment.Value)
			if err != nil {
				return err
			}
			values = append(values, value)
		}
		for _, value := range values {
			refs, err := collectV2References(value)
			if err != nil {
				return err
			}
			for _, ref := range refs {
				if ref.Alias == "vars" {
					parts, _ := jsonutil.PathSegments(ref.Path)
					if len(parts) > 1 && !parts[1].IsIndex {
						reads[step.ID][parts[1].Key] = true
					}
				}
			}
		}
	}
	for i, a := range def.Steps {
		for _, b := range def.Steps[i+1:] {
			exclusive := false
			for _, sa := range a.Scope {
				for _, sb := range b.Scope {
					if sa.ConditionID == sb.ConditionID && sa.BranchID != sb.BranchID {
						exclusive = true
					}
				}
			}
			if exclusive || closures[a.ID][b.ID] || closures[b.ID][a.ID] {
				continue
			}
			for name := range writes[a.ID] {
				if writes[b.ID][name] || reads[b.ID][name] {
					return fmt.Errorf("variable_order_ambiguous: %s between %s and %s", name, a.ID, b.ID)
				}
			}
			for name := range writes[b.ID] {
				if reads[a.ID][name] {
					return fmt.Errorf("variable_order_ambiguous: %s between %s and %s", name, a.ID, b.ID)
				}
			}
		}
	}
	return nil
}

func Transform(ctx context.Context, source any, operations []map[string]any, values map[string]any, metadata *RunMetadata) (any, []map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, StepTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if len(operations) < 1 || len(operations) > 10 {
		return nil, nil, fmt.Errorf("transform requires 1-10 operations")
	}
	raw, encodeErr := json.Marshal(source)
	if encodeErr != nil || len(raw) > MaxAccumulatedBody {
		return nil, nil, fmt.Errorf("transform source exceeds workflow data budget")
	}
	result, err := cloneJSON(source)
	if err != nil {
		return nil, nil, err
	}
	var summaries []map[string]any
	for index, op := range operations {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		items, list := result.([]any)
		if len(items) > 10000 {
			return nil, nil, fmt.Errorf("transform input exceeds 10000 items")
		}
		name, _ := op["op"].(string)
		before := len(items)
		fail := func(err error) (any, []map[string]any, error) {
			return nil, summaries, fmt.Errorf("operation %d (%s): %w", index, name, err)
		}
		switch name {
		case "count":
			if !list {
				return fail(fmt.Errorf("array required"))
			}
			result = json.Number(fmt.Sprint(len(items)))
		case "slice":
			if !list {
				return fail(fmt.Errorf("array required"))
			}
			offset, err := operationInteger(op, "offset", false)
			if err != nil {
				return fail(err)
			}
			limit, err := operationInteger(op, "limit", true)
			if err != nil {
				return fail(err)
			}
			if offset > len(items) {
				offset = len(items)
			}
			end := len(items)
			if limit < end-offset {
				end = offset + limit
			}
			result = items[offset:end]
		case "filter":
			if !list {
				return fail(fmt.Errorf("array required"))
			}
			raw, err := json.Marshal(op["condition"])
			if err != nil {
				return fail(err)
			}
			var config map[string]any
			if err := jsonutil.Unmarshal(raw, &config); err != nil {
				return fail(err)
			}
			condition, err := itemCondition(config)
			if err != nil {
				return fail(err)
			}
			filtered := make([]any, 0, len(items))
			for _, item := range items {
				if err := ctx.Err(); err != nil {
					return fail(err)
				}
				scope := map[string]any{}
				for k, v := range values {
					scope[k] = v
				}
				scope["item"] = item
				match, err := EvaluateV2(scope, condition, metadata)
				if err != nil {
					return fail(err)
				}
				if match {
					filtered = append(filtered, item)
				}
			}
			result = filtered
		case "sort":
			if !list {
				return fail(fmt.Errorf("array required"))
			}
			path, _ := op["path"].(string)
			typ, _ := op["valueType"].(string)
			direction, _ := op["direction"].(string)
			if typ != "number" && typ != "string" && typ != "datetime" || direction != "asc" && direction != "desc" {
				return fail(fmt.Errorf("explicit sort type and direction required"))
			}
			keys := make([]sortItem, len(items))
			for i, item := range items {
				if err := ctx.Err(); err != nil {
					return fail(err)
				}
				key := item
				found := true
				if path != "" {
					key, found = jsonutil.PathLookup(item, path)
				}
				keys[i] = sortItem{value: item, missing: !found || key == nil}
				if !keys[i].missing {
					switch typ {
					case "number":
						n, ok := key.(json.Number)
						if !ok {
							return fail(fmt.Errorf("numeric sort field required"))
						}
						rat, err := exactNumber(n)
						if err != nil {
							return fail(fmt.Errorf("invalid number"))
						}
						keys[i].number = rat
					case "string":
						text, ok := key.(string)
						if !ok {
							return fail(fmt.Errorf("string sort field required"))
						}
						keys[i].text = text
					case "datetime":
						text, ok := key.(string)
						if !ok {
							return fail(fmt.Errorf("RFC3339 sort field required"))
						}
						parsed, err := time.Parse(time.RFC3339Nano, text)
						if err != nil {
							return fail(fmt.Errorf("RFC3339 sort field required"))
						}
						keys[i].date = parsed
					}
				}
			}
			sort.SliceStable(keys, func(i, j int) bool {
				if ctx.Err() != nil {
					return false
				}
				a, b := keys[i], keys[j]
				if a.missing || b.missing {
					return !a.missing && b.missing
				}
				comparison := 0
				switch typ {
				case "number":
					comparison = a.number.Cmp(b.number)
				case "string":
					comparison = strings.Compare(a.text, b.text)
				case "datetime":
					comparison = a.date.Compare(b.date)
				}
				if direction == "desc" {
					return comparison > 0
				}
				return comparison < 0
			})
			sorted := make([]any, len(keys))
			for i, key := range keys {
				sorted[i] = key.value
			}
			result = sorted
		case "select":
			fields, ok := op["fields"].([]any)
			if !ok || len(fields) == 0 {
				return fail(fmt.Errorf("fields required"))
			}
			project := func(item any) (any, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				output := map[string]any{}
				for _, field := range fields {
					config, ok := field.(map[string]any)
					if !ok {
						return nil, fmt.Errorf("invalid projection field")
					}
					path, _ := config["path"].(string)
					as, _ := config["as"].(string)
					if as == "" {
						return nil, fmt.Errorf("output field name required")
					}
					if _, ok := output[as]; ok {
						return nil, fmt.Errorf("duplicate output field")
					}
					value, found := jsonutil.PathLookup(item, path)
					optional, _ := config["optional"].(bool)
					if !found && !optional {
						return nil, fmt.Errorf("projection field is missing")
					}
					output[as] = value
				}
				return output, nil
			}
			if list {
				projected := make([]any, len(items))
				for i, item := range items {
					projected[i], err = project(item)
					if err != nil {
						return fail(err)
					}
				}
				result = projected
			} else {
				if _, ok := result.(map[string]any); !ok {
					return fail(fmt.Errorf("object or object array required"))
				}
				result, err = project(result)
				if err != nil {
					return fail(err)
				}
			}
		default:
			return fail(fmt.Errorf("unknown transform operation"))
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		raw, err := json.Marshal(result)
		if err != nil || len(raw) > MaxAccumulatedBody {
			return fail(fmt.Errorf("transform result exceeds workflow data budget"))
		}
		summary := map[string]any{"op": name}
		if list {
			summary["inputCount"] = before
		}
		if output, ok := result.([]any); ok {
			summary["outputCount"] = len(output)
		}
		summaries = append(summaries, summary)
	}
	return map[string]any{"result": result}, summaries, nil
}

type sortItem struct {
	value   any
	missing bool
	number  *big.Rat
	text    string
	date    time.Time
}

func operationInteger(op map[string]any, key string, required bool) (int, error) {
	value, ok := op[key]
	if !ok && !required {
		return 0, nil
	}
	n, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("%s must be a nonnegative integer", key)
	}
	i, err := n.Int64()
	if err != nil || i < 0 || i > 1<<31 {
		return 0, fmt.Errorf("%s must be a nonnegative integer", key)
	}
	return int(i), nil
}
func itemCondition(config map[string]any) (*Condition, error) {
	if config == nil {
		return nil, fmt.Errorf("filter condition is required")
	}
	c := &Condition{}
	for key, value := range config {
		switch key {
		case "all", "any":
			items, ok := value.([]any)
			if !ok {
				return nil, fmt.Errorf("filter group must be an array")
			}
			var children []*Condition
			for _, item := range items {
				child, ok := item.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid filter condition")
				}
				parsed, err := itemCondition(child)
				if err != nil {
					return nil, err
				}
				children = append(children, parsed)
			}
			if key == "all" {
				c.All = children
			} else {
				c.Any = children
			}
		case "itemPath":
			path, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("invalid itemPath")
			}
			c.Path = "item"
			if path != "" {
				c.Path += "." + path
			}
		case "op":
			c.Op, _ = value.(string)
		case "value":
			c.Value = value
		default:
			return nil, fmt.Errorf("unknown filter condition field")
		}
	}
	if c.Path == "" && c.All == nil && c.Any == nil {
		c.Path = "item"
	}
	if err := ValidateV2Condition(c); err != nil {
		return nil, err
	}
	return c, nil
}

// Bound decimal expansion before allocating big integers, while preserving
// exact comparison for large JSON identifiers and monetary values.
func exactNumber(value any) (*big.Rat, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("numeric value required")
	}
	text := string(raw)
	if len(text) > 4096 {
		return nil, fmt.Errorf("number exceeds comparison limits")
	}
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(text[index+1:])
		if err != nil || exponent > 4096 || exponent < -4096 {
			return nil, fmt.Errorf("number exceeds comparison limits")
		}
	}
	switch value.(type) {
	case json.Number, float64, int, int64:
	default:
		return nil, fmt.Errorf("numeric value required")
	}
	number, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("numeric value required")
	}
	return number, nil
}

// EvaluateV2Traced records evaluation order without storing business values.
// A short circuit marks the remaining condition nodes as not evaluated.
func EvaluateV2Traced(values map[string]any, c *Condition, metadata *RunMetadata, path string) (bool, []map[string]any, error) {
	trace := []map[string]any{}
	var walk func(*Condition, string, bool) (bool, error)
	walk = func(node *Condition, p string, skip bool) (bool, error) {
		entry := map[string]any{"fieldPath": p, "status": "not_evaluated"}
		trace = append(trace, entry)
		if node == nil {
			if !skip {
				entry["status"] = "matched"
			}
			return !skip, nil
		}
		if node.Path != "" {
			entry["path"] = node.Path
			entry["op"] = node.Op
			if !skip && node.Op != "exists" && node.Op != "not_exists" {
				refs, _ := collectV2References(node.Value)
				paths := []string{}
				for _, ref := range refs {
					if ref.Alias == "vars" {
						paths = append(paths, ref.Path)
					}
				}
				entry["variablePaths"] = paths
			}
		}
		children, group := node.All, "all"
		if len(node.Any) > 0 {
			children, group = node.Any, "any"
		}
		if len(children) == 0 {
			if skip {
				return false, nil
			}
			match, err := EvaluateV2(values, node, metadata)
			entry["status"] = "not_matched"
			if err != nil {
				entry["status"] = "failed"
			} else if match {
				entry["status"] = "matched"
			}
			return match, err
		}
		stopped, result := skip, group == "all"
		var failure error
		for i, child := range children {
			match, err := walk(child, fmt.Sprintf("%s/%s/%d", p, group, i), stopped)
			if stopped {
				continue
			}
			if err != nil {
				failure = err
				stopped = true
				result = false
			} else if group == "all" && !match || group == "any" && match {
				result = match
				stopped = true
			}
		}
		if !skip {
			entry["status"] = "not_matched"
			if failure != nil {
				entry["status"] = "failed"
			} else if result {
				entry["status"] = "matched"
			}
		}
		return result, failure
	}
	match, err := walk(c, path, false)
	return match, trace, err
}

func equalV2(a, b any) (bool, error) {
	numeric := func(v any) bool {
		switch v.(type) {
		case json.Number, float64, float32, int, int64, int32, uint, uint64:
			return true
		}
		return false
	}
	if numeric(a) || numeric(b) {
		if !numeric(a) || !numeric(b) {
			return false, nil
		}
		left, err := exactNumber(a)
		if err != nil {
			return false, err
		}
		right, err := exactNumber(b)
		if err != nil {
			return false, err
		}
		return left.Cmp(right) == 0, nil
	}
	switch left := a.(type) {
	case map[string]any:
		right, ok := b.(map[string]any)
		if !ok || len(left) != len(right) {
			return false, nil
		}
		for k, v := range left {
			other, exists := right[k]
			if !exists {
				return false, nil
			}
			equal, err := equalV2(v, other)
			if err != nil || !equal {
				return false, err
			}
		}
		return true, nil
	case []any:
		right, ok := b.([]any)
		if !ok || len(left) != len(right) {
			return false, nil
		}
		for i, v := range left {
			equal, err := equalV2(v, right[i])
			if err != nil || !equal {
				return false, err
			}
		}
		return true, nil
	case string:
		right, ok := b.(string)
		return ok && left == right, nil
	case bool:
		right, ok := b.(bool)
		return ok && left == right, nil
	case nil:
		return b == nil, nil
	}
	return false, fmt.Errorf("condition requires JSON values")
}
