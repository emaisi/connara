package workflow

import (
	"apihub-go/internal/jsonutil"
	"fmt"
)

// Validate source availability in each possible branch, not just existence in
// the DAG. A branch marker narrows the context for only its selected case.
func validateScopedSources(def Definition, closures map[string]map[string]bool) error {
	byID := map[string]Step{}
	for _, step := range def.Steps {
		byID[step.ID] = step
	}
	prefix := func(source, scope []BranchScope) bool {
		if len(source) > len(scope) {
			return false
		}
		for i, s := range source {
			if scope[i] != s {
				return false
			}
		}
		return true
	}
	var check func(any, []BranchScope, map[string]bool, bool) error
	check = func(value any, scope []BranchScope, allowed map[string]bool, output bool) error {
		switch v := value.(type) {
		case string:
			refs, err := referencesInString(v)
			if err != nil {
				return err
			}
			for _, ref := range refs {
				if ref.Alias == "trigger" || ref.Alias == "vars" {
					continue
				}
				statusReference := ref.Alias == "status"
				if statusReference {
					parts, _ := jsonutil.PathSegments(ref.Path)
					if len(parts) > 1 {
						ref.Alias = parts[1].Key
					}
				}
				source, exists := byID[ref.Alias]
				if !exists || !allowed[source.ID] {
					return fmt.Errorf("source is outside dependency closure")
				}
				if !statusReference && !prefix(source.Scope, scope) && !(output && ref.Optional) {
					return fmt.Errorf("source from inactive branch requires a branch value")
				}
			}
		case map[string]any:
			marker, reserved, err := valueMarker(v)
			if err != nil {
				return err
			}
			if reserved {
				switch marker["kind"] {
				case "literal", "run":
					return nil
				case "branch":
					id := marker["conditionId"].(string)
					condition, exists := byID[id]
					if !exists || condition.Type != "condition" || !allowed[id] || !prefix(condition.Scope, scope) {
						return fmt.Errorf("branch value condition is unavailable")
					}
					cases := marker["cases"].(map[string]any)
					expected := map[string]bool{}
					for _, branch := range condition.Branches {
						active := true
						for _, current := range scope {
							if current.ConditionID == id && current.BranchID != branch.ID {
								active = false
							}
						}
						if !active {
							continue
						}
						expected[branch.ID] = true
						item, exists := cases[branch.ID]
						if !exists {
							return fmt.Errorf("branch value must cover every possible exit")
						}
						branchScope := append(append([]BranchScope{}, condition.Scope...), BranchScope{ConditionID: id, BranchID: branch.ID})
						if len(scope) > len(branchScope) && prefix(branchScope, scope) {
							branchScope = scope
						}
						if err := check(item, branchScope, allowed, output); err != nil {
							return err
						}
					}
					for id := range cases {
						known := false
						for _, branch := range condition.Branches {
							known = known || branch.ID == id
						}
						if !known {
							return fmt.Errorf("branch value references unknown exit")
						}
					}
					return nil
				}
			}
			for _, item := range v {
				if err := check(item, scope, allowed, output); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range v {
				if err := check(item, scope, allowed, output); err != nil {
					return err
				}
			}
		}
		return nil
	}
	normalize := func(value any) any { copy, _ := cloneJSON(value); return copy }
	for _, step := range def.Steps {
		allowed := closures[step.ID]
		for _, id := range step.DependsOn {
			dependency := byID[id]
			if !prefix(dependency.Scope, step.Scope) && !prefix(step.Scope, dependency.Scope) {
				return fmt.Errorf("step %q depends on a mutually exclusive branch", step.ID)
			}
		}
		values := []any{step.Input, step.Operations}
		if len(step.Source) > 0 {
			source, err := decodeValue(step.Source)
			if err != nil {
				return err
			}
			values = append(values, source)
		}
		for _, value := range values {
			if err := check(normalize(value), step.Scope, allowed, false); err != nil {
				return fmt.Errorf("step %q: %w", step.ID, err)
			}
		}
		for _, ref := range conditionReferences(step.RunIf) {
			if err := check("{{"+ref.Path+"}}", step.Scope, allowed, false); err != nil {
				return err
			}
		}
		post := map[string]bool{step.ID: true}
		for id := range allowed {
			post[id] = true
		}
		for _, assignment := range step.Assign {
			value, err := decodeValue(assignment.Value)
			if err != nil {
				return err
			}
			if err := check(value, step.Scope, post, false); err != nil {
				return err
			}
		}
		for _, branch := range step.Branches {
			for _, ref := range conditionReferences(branch.Condition) {
				if err := check("{{"+ref.Path+"}}", step.Scope, allowed, false); err != nil {
					return err
				}
			}
			if err := checkConditionValues(branch.Condition, func(value any) error { return check(value, step.Scope, allowed, false) }); err != nil {
				return err
			}
			for _, assignment := range branch.Assign {
				value, err := decodeValue(assignment.Value)
				if err != nil {
					return err
				}
				if err := check(value, step.Scope, allowed, false); err != nil {
					return err
				}
			}
		}
	}
	allowed := map[string]bool{}
	for id := range byID {
		allowed[id] = true
	}
	return check(def.Output, nil, allowed, true)
}
