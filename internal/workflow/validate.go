package workflow

import (
	"fmt"
	"strings"

	"apihub-go/internal/jsonutil"
)

// Reference is one {{path}} or {{?path}} occurrence inside a template value.
type Reference struct {
	Path     string
	Alias    string
	Optional bool
}

// ValidateDefinition enforces the structural contract of a graph: alias
// rules, dependency existence, acyclicity, reference closures, restricted
// conditions and onError values. requireDeploy additionally demands at least
// one step, bound integrations/connections and every referenced action to be
// resolvable by key through the lookup callback.
func ValidateDefinition(definition Definition, requireDeploy bool, actionExists func(key string) bool) error {
	if definition.SchemaVersion == 2 {
		return validateV2Definition(definition, requireDeploy, actionExists)
	}
	if definition.SchemaVersion != 0 && definition.SchemaVersion != 1 {
		return fmt.Errorf("unsupported graph schemaVersion %d", definition.SchemaVersion)
	}
	if len(definition.InputSchema) > 0 || len(definition.Variables) > 0 {
		return fmt.Errorf("inputSchema and variables require graph schemaVersion 2")
	}
	if len(definition.Steps) > MaxSteps {
		return fmt.Errorf("workflow exceeds %d steps", MaxSteps)
	}
	if requireDeploy {
		if len(definition.Steps) == 0 {
			return fmt.Errorf("workflow has no steps")
		}
		if len(definition.Output) == 0 {
			return fmt.Errorf("workflow output mapping is required before deploy")
		}
	}
	identifiers := map[string]bool{}
	for index, step := range definition.Steps {
		if step.Type != "" || len(step.Scope) > 0 || len(step.Assign) > 0 || len(step.Source) > 0 || len(step.Operations) > 0 || len(step.Branches) > 0 || step.Language != "" || step.RuntimeProfile != "" || step.Code != "" || len(step.InputSchema) > 0 || len(step.OutputSchema) > 0 {
			return fmt.Errorf("step %q uses fields that require graph schemaVersion 2", step.ID)
		}
		if !validStepID(step.ID) {
			return fmt.Errorf("step %d has invalid id %q: use 1-64 letters, digits, - or _", index, step.ID)
		}
		if step.ID == "trigger" || step.ID == "status" {
			return fmt.Errorf("step id %q is reserved", step.ID)
		}
		if identifiers[step.ID] {
			return fmt.Errorf("step id %q is used more than once", step.ID)
		}
		identifiers[step.ID] = true
		if strings.TrimSpace(step.Action) == "" {
			return fmt.Errorf("step %q has no action", step.ID)
		}
		if requireDeploy && step.IntegrationID == "" {
			return fmt.Errorf("step %q must bind an explicit integration before deploy", step.ID)
		}
		if step.OnError != "" && step.OnError != "fail" && step.OnError != "continue" {
			return fmt.Errorf("step %q has unsupported onError %q", step.ID, step.OnError)
		}
		if err := ValidateCondition(step.RunIf); err != nil {
			return fmt.Errorf("step %q runIf: %w", step.ID, err)
		}
		if actionExists != nil && !actionExists(step.Action) {
			return fmt.Errorf("step %q references unknown action %q", step.ID, step.Action)
		}
		seen := map[string]bool{}
		for _, dependency := range step.DependsOn {
			if dependency == step.ID {
				return fmt.Errorf("step %q depends on itself", step.ID)
			}
			if seen[dependency] {
				return fmt.Errorf("step %q declares dependency %q twice", step.ID, dependency)
			}
			seen[dependency] = true
		}
	}
	order, err := OrderSteps(definition.Steps)
	if err != nil {
		return err
	}
	// Dependency closure per step, used to confine template references.
	closures := map[string]map[string]bool{}
	for _, index := range order {
		step := definition.Steps[index]
		closure := map[string]bool{}
		for _, dependency := range step.DependsOn {
			closure[dependency] = true
			for ancestor := range closures[dependency] {
				closure[ancestor] = true
			}
		}
		closures[step.ID] = closure
		references, err := collectStepReferences(step)
		if err != nil {
			return fmt.Errorf("step %q input: %w", step.ID, err)
		}
		for _, reference := range references {
			if reference.Optional {
				return fmt.Errorf("step %q uses optional reference {{?%s}}; only the workflow output mapping may omit missing paths", step.ID, reference.Path)
			}
			if err := checkReference(reference, closure, fmt.Sprintf("step %q", step.ID), false); err != nil {
				return err
			}
		}
	}
	for key, value := range definition.Output {
		references, err := CollectReferences(value)
		if err != nil {
			return fmt.Errorf("output %q: %w", key, err)
		}
		for _, reference := range references {
			if err := checkReference(reference, identifiers, fmt.Sprintf("output %q", key), true); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkReference validates one reference against the allowed aliases. In
// output context every declared alias is allowed; in step context only the
// dependency closure is. status.<alias> resolves the alias from the second
// path segment.
func checkReference(reference Reference, allowed map[string]bool, where string, output bool) error {
	if reference.Alias == "" {
		return fmt.Errorf("%s references %q which has no leading alias", where, reference.Path)
	}
	switch reference.Alias {
	case "trigger":
		return nil
	case "status":
		segments, _ := jsonutil.PathSegments(reference.Path)
		if len(segments) < 2 || segments[1].IsIndex {
			return fmt.Errorf("%s references status without a step alias", where)
		}
		if !allowed[segments[1].Key] {
			return fmt.Errorf("%s references status of %q which is not an available step", where, segments[1].Key)
		}
		return nil
	default:
		if !allowed[reference.Alias] {
			if output {
				return fmt.Errorf("%s references step %q which is not declared", where, reference.Alias)
			}
			return fmt.Errorf("%s references step %q outside of its dependsOn closure", where, reference.Alias)
		}
		return nil
	}
}

func validStepID(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

// OrderSteps returns indexes of the steps in stable topological order: among
// steps whose dependencies are all placed, declaration order wins, so event
// sequences stay deterministic.
func OrderSteps(steps []Step) ([]int, error) {
	identifiers := make([]string, len(steps))
	dependencies := make([][]string, len(steps))
	for index, step := range steps {
		identifiers[index] = step.ID
		dependencies[index] = step.DependsOn
	}
	return orderGraph(identifiers, dependencies)
}

// OrderSnapshots is OrderSteps for pinned run snapshots.
func OrderSnapshots(steps []StepSnapshot) ([]StepSnapshot, error) {
	identifiers := make([]string, len(steps))
	dependencies := make([][]string, len(steps))
	for index, step := range steps {
		identifiers[index] = step.ID
		dependencies[index] = step.DependsOn
	}
	order, err := orderGraph(identifiers, dependencies)
	if err != nil {
		return nil, err
	}
	ordered := make([]StepSnapshot, 0, len(steps))
	for _, index := range order {
		ordered = append(ordered, steps[index])
	}
	return ordered, nil
}

// orderGraph runs a stable Kahn sort: identifiers must be unique and every
// dependency must exist; cycles are rejected.
func orderGraph(identifiers []string, dependencies [][]string) ([]int, error) {
	total := len(identifiers)
	position := map[string]int{}
	for index, identifier := range identifiers {
		if _, duplicate := position[identifier]; duplicate {
			return nil, fmt.Errorf("step %q is declared twice", identifier)
		}
		position[identifier] = index
	}
	for index, identifier := range identifiers {
		for _, dependency := range dependencies[index] {
			if _, ok := position[dependency]; !ok {
				return nil, fmt.Errorf("step %q depends on unknown step %q", identifier, dependency)
			}
		}
	}
	placed := make([]bool, total)
	order := make([]int, 0, total)
	for len(order) < total {
		chosen := -1
		for index := 0; index < total; index++ {
			if placed[index] {
				continue
			}
			ready := true
			for _, dependency := range dependencies[index] {
				if !placed[position[dependency]] {
					ready = false
					break
				}
			}
			if ready {
				chosen = index
				break
			}
		}
		if chosen < 0 {
			return nil, fmt.Errorf("workflow dependency graph contains a cycle")
		}
		placed[chosen] = true
		order = append(order, chosen)
	}
	return order, nil
}

// collectStepReferences gathers references from a step's input template and
// runIf path.
func collectStepReferences(step Step) ([]Reference, error) {
	references, err := CollectReferences(step.Input)
	if err != nil {
		return nil, err
	}
	if step.RunIf != nil && step.RunIf.Path != "" {
		references = append(references, Reference{Path: step.RunIf.Path, Alias: aliasOf(step.RunIf.Path)})
	}
	// runIf groups embed further leaf paths.
	references = append(references, conditionReferences(step.RunIf)...)
	return references, nil
}

func conditionReferences(condition *Condition) []Reference {
	if condition == nil {
		return nil
	}
	var references []Reference
	if condition.Path != "" {
		references = append(references, Reference{Path: condition.Path, Alias: aliasOf(condition.Path)})
	}
	for _, item := range condition.All {
		references = append(references, conditionReferences(item)...)
	}
	for _, item := range condition.Any {
		references = append(references, conditionReferences(item)...)
	}
	return references
}

// CollectReferences walks a template value and returns every placeholder
// reference with strict syntax validation.
func CollectReferences(value any) ([]Reference, error) {
	var references []Reference
	var walk func(item any) error
	walk = func(item any) error {
		switch typed := item.(type) {
		case string:
			found, err := referencesInString(typed)
			if err != nil {
				return err
			}
			references = append(references, found...)
		case map[string]any:
			for _, nested := range typed {
				if err := walk(nested); err != nil {
					return err
				}
			}
		case []any:
			for _, nested := range typed {
				if err := walk(nested); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(value); err != nil {
		return nil, err
	}
	return references, nil
}

func referencesInString(text string) ([]Reference, error) {
	if whole, ok := exactPlaceholder(text); ok {
		return []Reference{{Path: whole.path, Alias: aliasOf(whole.path), Optional: whole.optional}}, nil
	}
	var references []Reference
	remaining := text
	for {
		start := strings.Index(remaining, "{{")
		if start < 0 {
			return references, nil
		}
		end := strings.Index(remaining[start+2:], "}}")
		if end < 0 {
			return nil, fmt.Errorf("template text %q has an unterminated placeholder", text)
		}
		expression := remaining[start+2 : start+2+end]
		if expression == "" || strings.ContainsAny(expression, "{}?") {
			return nil, fmt.Errorf("template placeholder {{%s}} in %q is malformed", expression, text)
		}
		if _, valid := jsonutil.PathSegments(expression); !valid {
			return nil, fmt.Errorf("template placeholder {{%s}} in %q is malformed", expression, text)
		}
		references = append(references, Reference{Path: expression, Alias: aliasOf(expression)})
		remaining = remaining[start+2+end+2:]
	}
}

func aliasOf(path string) string {
	segments, ok := jsonutil.PathSegments(path)
	if !ok || len(segments) == 0 || segments[0].IsIndex {
		return ""
	}
	return segments[0].Key
}
