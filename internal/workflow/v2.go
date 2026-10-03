package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"apihub-go/internal/catalog"
	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
)

func validateV2Definition(definition Definition, deploy bool, actionExists func(string) bool) (result error) {
	stepID, phase, path := "", "definition", "/graph"
	defer func() { result = Located(result, "invalid_graph", stepID, phase, path) }()
	if len(definition.Steps) > MaxSteps {
		return fmt.Errorf("workflow exceeds %d steps", MaxSteps)
	}
	if len(definition.Variables) > 32 {
		return fmt.Errorf("workflow exceeds 32 variables")
	}
	path = "/graph/inputSchema"
	if len(definition.InputSchema) > 0 {
		if err := validateNodeSchema(definition.InputSchema, true, 1); err != nil {
			return fmt.Errorf("inputSchema: %w", err)
		}
	}
	variables := map[string]bool{}
	for index, variable := range definition.Variables {
		path = fmt.Sprintf("/graph/variables/%d/initial", index)
		if !validVariableName(variable.Name) || variables[variable.Name] || !validJSONType(variable.Type) || len(variable.Initial) == 0 {
			return fmt.Errorf("invalid variable declaration %q", variable.Name)
		}
		variables[variable.Name] = true
		value, err := decodeValue(variable.Initial)
		if err != nil {
			return err
		}
		references, err := collectV2References(value)
		if err != nil {
			return err
		}
		for _, ref := range references {
			if ref.Alias != "trigger" {
				return fmt.Errorf("variable %q initial may only reference trigger or run information", variable.Name)
			}
		}
	}
	identifiers := map[string]bool{}
	byID := map[string]Step{}
	apis := 0
	for _, step := range definition.Steps {
		stepID, phase, path = step.ID, "definition", "/id"
		if !validStepID(step.ID) || identifiers[step.ID] || step.ID == "trigger" || step.ID == "status" || step.ID == "vars" {
			return fmt.Errorf("invalid or reserved step id %q", step.ID)
		}
		identifiers[step.ID], byID[step.ID] = true, step
		if step.OnError != "" && step.OnError != "fail" && (step.OnError != "continue" || !step.IsAPI()) {
			return fmt.Errorf("step %q has unsupported onError", step.ID)
		}
		if !step.IsAPI() && step.RunIf != nil {
			return fmt.Errorf("step %q: runIf is only supported on API nodes", step.ID)
		}
		if err := ValidateCondition(step.RunIf); err != nil {
			return fmt.Errorf("step %q runIf: %w", step.ID, err)
		}
		seen := map[string]bool{}
		for _, dep := range step.DependsOn {
			if dep == step.ID || seen[dep] {
				return fmt.Errorf("step %q has repeated or self dependency", step.ID)
			}
			seen[dep] = true
		}
		path = "/type"
		switch step.Type {
		case "api":
			apis++
			path = "/action"
			if strings.TrimSpace(step.Action) == "" || (deploy && step.IntegrationID == "") {
				return fmt.Errorf("step %q requires an action and deployed integration binding", step.ID)
			}
			if actionExists != nil && !actionExists(step.Action) {
				return fmt.Errorf("step %q references an unknown action", step.ID)
			}
			if len(step.Source) > 0 || len(step.Operations) > 0 || len(step.Branches) > 0 || step.Code != "" || step.Language != "" || step.RuntimeProfile != "" || len(step.InputSchema) > 0 || len(step.OutputSchema) > 0 {
				return fmt.Errorf("API step %q contains local node fields", step.ID)
			}
		case "transform", "condition", "code":
			if step.Action != "" || step.IntegrationID != "" || step.ConnectionKey != "" {
				return fmt.Errorf("local step %q cannot bind an API", step.ID)
			}
			if step.Type != "code" && (step.Code != "" || step.Language != "" || step.RuntimeProfile != "" || len(step.InputSchema) > 0 || len(step.OutputSchema) > 0 || len(step.Input) > 0) || step.Type != "transform" && (len(step.Source) > 0 || len(step.Operations) > 0) || step.Type != "condition" && len(step.Branches) > 0 {
				return fmt.Errorf("step %q contains fields for another node type", step.ID)
			}
			if step.Type == "code" {
				phase, path = "code", "/code"
				if step.Language != "javascript" || step.RuntimeProfile != "js-v1" || len(step.Code) > 32<<10 {
					return fmt.Errorf("code step %q requires javascript/js-v1 and source within 32 KiB", step.ID)
				}
				for index, schema := range []map[string]any{step.InputSchema, step.OutputSchema} {
					path = []string{"/inputSchema", "/outputSchema"}[index]
					if err := validateNodeSchema(schema, true, 1); err != nil {
						return fmt.Errorf("code step %q schema: %w", step.ID, err)
					}
					properties, _ := schema["properties"].(map[string]any)
					var required []string
					raw, _ := json.Marshal(schema["required"])
					_ = json.Unmarshal(raw, &required)
					seen := map[string]bool{}
					for _, name := range required {
						seen[name] = true
					}
					if len(seen) != len(properties) {
						return fmt.Errorf("code step %q named fields must all be required", step.ID)
					}
					for name := range properties {
						if !seen[name] {
							return fmt.Errorf("code step %q named fields must all be required", step.ID)
						}
					}
				}
				phase, path = "input", "/input"
				properties, _ := step.InputSchema["properties"].(map[string]any)
				if len(properties) != len(step.Input) {
					return fmt.Errorf("code step %q input mapping must match named inputs", step.ID)
				}
				for name := range properties {
					if _, exists := step.Input[name]; !exists {
						return fmt.Errorf("code step %q has missing input mapping", step.ID)
					}
				}
			}
			if step.Type == "condition" {
				phase, path = "condition", "/branches"
				if len(step.Assign) != 0 {
					return fmt.Errorf("condition assignments belong to branches")
				}
				if len(step.Branches) < 2 || len(step.Branches) > 9 {
					return fmt.Errorf("condition step %q needs IF and ELSE, up to 9 branches", step.ID)
				}
				branches := map[string]bool{}
				for index, branch := range step.Branches {
					path = fmt.Sprintf("/branches/%d/condition", index)
					if !validStepID(branch.ID) || branches[branch.ID] || branch.Default != (index == len(step.Branches)-1) || (branch.Default && branch.Condition != nil) || (!branch.Default && branch.Condition == nil) {
						return fmt.Errorf("condition step %q has invalid branches", step.ID)
					}
					if !branch.Default {
						if err := ValidateV2Condition(branch.Condition); err != nil {
							return fmt.Errorf("condition step %q: %w", step.ID, err)
						}
					}
					branches[branch.ID] = true
				}
			}
			if step.Type == "transform" && (len(step.Source) == 0 || len(step.Operations) < 1 || len(step.Operations) > 10) {
				return fmt.Errorf("transform step %q requires source and 1-10 operations", step.ID)
			}
			if step.Type == "transform" {
				phase, path = "transform", "/operations"
				if err := validateOperations(step.Operations); err != nil {
					return fmt.Errorf("step %q: %w", step.ID, err)
				}
			}
		default:
			return fmt.Errorf("unknown v2 step type %q", step.Type)
		}
		if raw, err := json.Marshal(step.Input); err != nil || len(raw) > MaxInputBytes {
			return fmt.Errorf("step %q input mapping exceeds 64 KiB", step.ID)
		}
	}
	if deploy && (apis == 0 || len(definition.Output) == 0) {
		return fmt.Errorf("v2 deployment requires at least one API and an output mapping")
	}
	order, err := OrderSteps(definition.Steps)
	if err != nil {
		return err
	}
	closures := map[string]map[string]bool{}
	check := func(value any, allowed map[string]bool, where string, output bool) error {
		refs, err := collectV2References(value)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		for _, ref := range refs {
			if ref.Optional && !output {
				return fmt.Errorf("%s uses an optional reference outside output", where)
			}
			if ref.Alias == "vars" {
				segments, _ := jsonutil.PathSegments(ref.Path)
				if len(segments) < 2 || segments[1].IsIndex || !variables[segments[1].Key] {
					return fmt.Errorf("%s references an undeclared variable", where)
				}
				continue
			}
			if err := checkReference(ref, allowed, where, output); err != nil {
				return err
			}
		}
		return nil
	}
	assignments := func(assign []Assignment, allowed map[string]bool, where string) error {
		seen := map[string]bool{}
		for _, assignment := range assign {
			if !variables[assignment.Variable] || seen[assignment.Variable] || len(assignment.Value) == 0 {
				return fmt.Errorf("%s has invalid variable assignment", where)
			}
			seen[assignment.Variable] = true
			value, err := decodeValue(assignment.Value)
			if err != nil {
				return err
			}
			if err := check(value, allowed, where, false); err != nil {
				return err
			}
		}
		return nil
	}
	for _, index := range order {
		step := definition.Steps[index]
		stepID, phase, path = step.ID, "branch", "/scope"
		closure := map[string]bool{}
		for _, dep := range step.DependsOn {
			closure[dep] = true
			for ancestor := range closures[dep] {
				closure[ancestor] = true
			}
		}
		closures[step.ID] = closure
		for index, scope := range step.Scope {
			condition, ok := byID[scope.ConditionID]
			if !ok || condition.Type != "condition" || !closure[scope.ConditionID] || len(condition.Scope) != index {
				return fmt.Errorf("step %q has invalid condition scope", step.ID)
			}
			for i := 0; i < index; i++ {
				if condition.Scope[i] != step.Scope[i] {
					return fmt.Errorf("step %q has inconsistent scope ancestry", step.ID)
				}
			}
			found := false
			for _, branch := range condition.Branches {
				found = found || branch.ID == scope.BranchID
			}
			if !found {
				return fmt.Errorf("step %q references an unknown branch", step.ID)
			}
		}
		where := fmt.Sprintf("step %q", step.ID)
		phase, path = "input", "/input"
		if err := check(step.Input, closure, where, false); err != nil {
			return err
		}
		for _, ref := range conditionReferences(step.RunIf) {
			if err := checkReference(ref, closure, where, false); err != nil && ref.Alias != "vars" {
				return err
			}
			if ref.Alias == "vars" {
				if err := check("{{"+ref.Path+"}}", closure, where, false); err != nil {
					return err
				}
			}
		}
		phase, path = "transform", "/source"
		if len(step.Source) > 0 {
			value, err := decodeValue(step.Source)
			if err != nil {
				return err
			}
			if err := check(value, closure, where, false); err != nil {
				return err
			}
		}
		phase, path = "transform", "/operations"
		if err := check(step.Operations, closure, where, false); err != nil {
			return err
		}
		post := map[string]bool{step.ID: true}
		for id := range closure {
			post[id] = true
		}
		phase, path = "variables", "/assign"
		if err := assignments(step.Assign, post, where); err != nil {
			return err
		}
		for index, branch := range step.Branches {
			phase, path = "condition", fmt.Sprintf("/branches/%d/condition", index)
			for _, ref := range conditionReferences(branch.Condition) {
				if err := check("{{"+ref.Path+"}}", closure, where, false); err != nil {
					return err
				}
			}
			if err := checkConditionValues(branch.Condition, func(value any) error { return check(value, closure, where, false) }); err != nil {
				return err
			}
			phase, path = "variables", fmt.Sprintf("/branches/%d/assign", index)
			if err := assignments(branch.Assign, closure, where); err != nil {
				return err
			}
		}
	}
	stepID, phase, path = "", "output", "/graph/output"
	if err := check(definition.Output, identifiers, "output", true); err != nil {
		return err
	}

	phase, path = "branch", "/graph/steps"
	if err := validateScopedSources(definition, closures); err != nil {
		return err
	}
	phase, path = "variables", "/graph/variables"
	if err := validateVariableOrder(definition, closures); err != nil {
		return err
	}
	return nil
}

func decodeValue(data json.RawMessage) (any, error) {
	var value any
	err := jsonutil.Unmarshal(data, &value)
	return value, err
}

func collectV2References(value any) ([]Reference, error) {
	// Normalize typed slices/maps while retaining json.Number and literal JSON.
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	value, err = decodeValue(raw)
	if err != nil {
		return nil, err
	}
	var refs []Reference
	var walk func(any, int) error
	walk = func(item any, depth int) error {
		if depth > 16 {
			return fmt.Errorf("value nesting exceeds 16 levels")
		}
		switch typed := item.(type) {
		case string:
			found, err := referencesInString(typed)
			if err != nil {
				return err
			}
			refs = append(refs, found...)
		case map[string]any:
			marker, reserved, err := valueMarker(typed)
			if err != nil {
				return err
			}
			if reserved {
				switch marker["kind"] {
				case "literal", "run":
					return nil
				case "branch":
					id := marker["conditionId"].(string)
					refs = append(refs, Reference{Path: id + ".branchId", Alias: id})
					return walk(marker["cases"], depth+1)
				}
			}
			for _, item := range typed {
				if err := walk(item, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range typed {
				if err := walk(item, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err = walk(value, 1)
	return refs, err
}

func checkConditionValues(condition *Condition, check func(any) error) error {
	if condition == nil {
		return nil
	}
	if err := check(condition.Value); err != nil {
		return err
	}
	for _, item := range condition.All {
		if err := checkConditionValues(item, check); err != nil {
			return err
		}
	}
	for _, item := range condition.Any {
		if err := checkConditionValues(item, check); err != nil {
			return err
		}
	}
	return nil
}

func validVariableName(name string) bool {
	if len(name) < 1 || len(name) > 64 || !((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) {
		return false
	}
	for _, char := range name {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_') {
			return false
		}
	}
	return true
}

func validJSONType(value string) bool {
	return value == "string" || value == "number" || value == "integer" || value == "boolean" || value == "object" || value == "array"
}

// The workflow subset forbids references/URLs and arbitrary unions. Runtime
// value validation still uses the existing JSON Schema compiler.
func validateNodeSchema(schema map[string]any, root bool, depth int) error {
	if depth > 16 {
		return fmt.Errorf("schema nesting exceeds 16 levels")
	}
	for key := range schema {
		if key != "type" && key != "properties" && key != "required" && key != "items" && key != "description" {
			return fmt.Errorf("unsupported schema keyword %q", key)
		}
	}
	base, ok := schema["type"].(string)
	if types, union := schema["type"].([]any); union {
		if root || len(types) != 2 {
			return fmt.Errorf("schema union must be one base type and null")
		}
		for _, item := range types {
			if text, ok := item.(string); ok && text != "null" {
				base = text
			}
		}
		if (types[0] != "null" && types[1] != "null") || !validJSONType(base) {
			return fmt.Errorf("invalid nullable schema type")
		}
		ok = true
	}
	if !ok || !validJSONType(base) || root && base != "object" {
		return fmt.Errorf("schema requires a valid type; root must be object")
	}
	if props, exists := schema["properties"]; exists {
		properties, ok := props.(map[string]any)
		if !ok || base != "object" {
			return fmt.Errorf("properties require an object schema")
		}
		for _, value := range properties {
			child, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("property schema must be an object")
			}
			if err := validateNodeSchema(child, false, depth+1); err != nil {
				return err
			}
		}
	}
	if items, exists := schema["items"]; exists {
		child, ok := items.(map[string]any)
		if !ok || base != "array" {
			return fmt.Errorf("items require an array schema")
		}
		if err := validateNodeSchema(child, false, depth+1); err != nil {
			return err
		}
	}
	// Compiling through the existing validator also catches malformed required
	// arrays and other invalid JSON Schema declarations, without a new engine.
	raw, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	return executor.ValidateSchema(raw)
}

func ValidateTrigger(schema map[string]any, trigger map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	return (&catalog.Catalog{}).ValidateInput(model.Action{ID: "workflow-trigger", InputSchema: schema}, trigger)
}
