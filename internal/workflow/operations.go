package workflow

import (
	"apihub-go/internal/jsonutil"
	"fmt"
)

func validateOperations(operations []map[string]any) error {
	counted := false
	for _, op := range operations {
		name, _ := op["op"].(string)
		if counted {
			return fmt.Errorf("array operations cannot follow count")
		}
		allowed := map[string]bool{"op": true}
		switch name {
		case "count":
			counted = true
		case "slice":
			allowed["offset"], allowed["limit"] = true, true
			if _, err := operationInteger(op, "offset", false); err != nil {
				return err
			}
			if _, err := operationInteger(op, "limit", true); err != nil {
				return err
			}
		case "sort":
			allowed["path"], allowed["valueType"], allowed["direction"] = true, true, true
			path, ok := op["path"].(string)
			if !ok {
				return fmt.Errorf("sort path is required")
			}
			if path != "" {
				if _, ok := jsonutil.PathSegments(path); !ok {
					return fmt.Errorf("invalid sort path")
				}
			}
			kind, _ := op["valueType"].(string)
			direction, _ := op["direction"].(string)
			if kind != "number" && kind != "string" && kind != "datetime" || direction != "asc" && direction != "desc" {
				return fmt.Errorf("invalid sort type or direction")
			}
		case "filter":
			allowed["condition"] = true
			condition, ok := op["condition"].(map[string]any)
			if !ok {
				return fmt.Errorf("filter condition required")
			}
			if _, err := itemCondition(condition); err != nil {
				return err
			}
		case "select":
			allowed["fields"] = true
			fields, ok := op["fields"].([]any)
			if !ok || len(fields) == 0 {
				return fmt.Errorf("projection fields required")
			}
			seen := map[string]bool{}
			for _, field := range fields {
				config, ok := field.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid projection field")
				}
				path, ok := config["path"].(string)
				if !ok {
					return fmt.Errorf("projection path required")
				}
				if _, ok := jsonutil.PathSegments(path); !ok {
					return fmt.Errorf("invalid projection path")
				}
				as, ok := config["as"].(string)
				if !ok || as == "" || seen[as] {
					return fmt.Errorf("invalid or duplicate output name")
				}
				if optional, exists := config["optional"]; exists {
					if _, ok := optional.(bool); !ok {
						return fmt.Errorf("projection optional must be boolean")
					}
				}
				seen[as] = true
				for key := range config {
					if key != "path" && key != "as" && key != "optional" {
						return fmt.Errorf("unknown projection field")
					}
				}
			}
		default:
			return fmt.Errorf("unknown transform operation")
		}
		for key := range op {
			if !allowed[key] {
				return fmt.Errorf("unknown transform configuration field %q", key)
			}
		}
	}
	return nil
}
