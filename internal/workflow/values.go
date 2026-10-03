package workflow

import (
	"fmt"
	"time"

	"apihub-go/internal/cronx"
)

// RunMetadata is frozen when a run is accepted, before queueing or variable
// initialization. It deliberately does not reserve a template alias.
type RunMetadata struct {
	TriggeredAt  time.Time  `json:"triggeredAt"`
	ScheduledFor *time.Time `json:"scheduledFor"`
	Timezone     string     `json:"timezone"`
	BusinessDate string     `json:"businessDate"`
}

func NewRunMetadata(triggeredAt time.Time, scheduledFor *time.Time, timezone string) (*RunMetadata, error) {
	if triggeredAt.IsZero() {
		return nil, fmt.Errorf("triggeredAt is required")
	}
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := cronx.LoadTimezone(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid run timezone: %w", err)
	}
	metadata := &RunMetadata{TriggeredAt: triggeredAt.UTC(), Timezone: timezone}
	basis := metadata.TriggeredAt
	if scheduledFor != nil {
		if scheduledFor.IsZero() {
			return nil, fmt.Errorf("scheduledFor must be a valid time or null")
		}
		planned := scheduledFor.UTC()
		metadata.ScheduledFor = &planned
		basis = planned
	}
	metadata.BusinessDate = basis.In(location).Format("2006-01-02")
	return metadata, nil
}

func validRunField(field string) bool {
	return field == "triggeredAt" || field == "scheduledFor" || field == "timezone" || field == "businessDate"
}

func (m *RunMetadata) field(field string) (any, error) {
	if !validRunField(field) {
		return nil, fmt.Errorf("unknown run metadata field %q", field)
	}
	if m == nil {
		return nil, fmt.Errorf("run metadata was not recorded")
	}
	switch field {
	case "triggeredAt":
		return m.TriggeredAt.Format(time.RFC3339Nano), nil
	case "scheduledFor":
		if m.ScheduledFor == nil {
			return nil, nil
		}
		return m.ScheduledFor.Format(time.RFC3339Nano), nil
	case "timezone":
		return m.Timezone, nil
	default:
		return m.BusinessDate, nil
	}
}

// Only an exact single-key object is reserved in v2. Literal values bypass
// recursion; branch cases are resolved lazily, including legitimate nulls.
func valueMarker(value map[string]any) (map[string]any, bool, error) {
	if len(value) != 1 {
		return nil, false, nil
	}
	marker, found := value["$value"]
	if !found {
		return nil, false, nil
	}
	config, ok := marker.(map[string]any)
	if !ok {
		return nil, true, fmt.Errorf("$value must be an object")
	}
	kind, _ := config["kind"].(string)
	keys := map[string]bool{"kind": true}
	switch kind {
	case "literal":
		keys["value"] = true
		if _, ok := config["value"]; !ok {
			return nil, true, fmt.Errorf("literal value is required")
		}
	case "run":
		keys["field"] = true
		field, _ := config["field"].(string)
		if !validRunField(field) {
			return nil, true, fmt.Errorf("invalid run metadata field")
		}
	case "branch":
		keys["conditionId"], keys["cases"] = true, true
		id, _ := config["conditionId"].(string)
		cases, ok := config["cases"].(map[string]any)
		if !validStepID(id) || !ok || len(cases) == 0 {
			return nil, true, fmt.Errorf("branch requires conditionId and cases")
		}
	default:
		return nil, true, fmt.Errorf("unknown $value kind %q", kind)
	}
	for key := range config {
		if !keys[key] {
			return nil, true, fmt.Errorf("unknown $value field %q", key)
		}
	}
	return config, true, nil
}

func ResolveV2(context map[string]any, value any, metadata *RunMetadata, output bool) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		marker, reserved, err := valueMarker(typed)
		if err != nil {
			return nil, err
		}
		if reserved {
			switch marker["kind"] {
			case "literal":
				return marker["value"], nil
			case "run":
				return metadata.field(marker["field"].(string))
			case "branch":
				condition, ok := context[marker["conditionId"].(string)].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("branch condition output is unavailable")
				}
				branchID, _ := condition["branchId"].(string)
				selected, found := marker["cases"].(map[string]any)[branchID]
				if !found {
					return nil, fmt.Errorf("selected branch has no value mapping")
				}
				return ResolveV2(context, selected, metadata, output)
			}
		}
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			resolved, err := ResolveV2(context, item, metadata, output)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			resolved, err := ResolveV2(context, item, metadata, output)
			if err != nil {
				return nil, err
			}
			result[index] = resolved
		}
		return result, nil
	default:
		return resolveValue(context, value, output)
	}
}
