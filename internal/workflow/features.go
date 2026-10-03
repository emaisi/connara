package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
)

var ErrFeatureDisabled = errors.New("workflow capability is disabled")

type Features struct {
	V2Enabled   bool
	CodeEnabled bool
}

func (f Features) CheckNew(graph json.RawMessage) error {
	def, err := ParseDefinition(graph)
	if err != nil {
		return err
	}
	return f.CheckDefinition(def)
}
func (f Features) CheckEditingDefinition(def Definition) error {
	if def.SchemaVersion == 2 && !f.V2Enabled {
		return fmt.Errorf("%w: v2", ErrFeatureDisabled)
	}
	return nil
}
func (f Features) CheckDefinition(def Definition) error {
	if err := f.CheckEditingDefinition(def); err != nil {
		return err
	}
	for _, step := range def.Steps {
		if step.Type == "code" && !f.CodeEnabled {
			return fmt.Errorf("%w: code", ErrFeatureDisabled)
		}
	}
	return nil
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Layout struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Direction     string              `json:"direction"`
	Positions     map[string]Position `json:"positions"`
}

func NormalizeLayout(raw json.RawMessage, definition Definition, prune bool) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "{}" {
		return json.RawMessage(`{}`), nil
	}
	if len(raw) > 32<<10 {
		return nil, fmt.Errorf("layout exceeds 32 KiB")
	}
	var layout Layout
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&layout); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("invalid layout JSON")
	}
	if layout.SchemaVersion != 1 || layout.Direction != "LR" || len(layout.Positions) > 22 {
		return nil, fmt.Errorf("unsupported layout")
	}
	allowed := map[string]bool{"@trigger": true, "@result": true}
	for _, step := range definition.Steps {
		allowed[step.ID] = true
	}
	for id, pos := range layout.Positions {
		if !allowed[id] {
			if prune {
				delete(layout.Positions, id)
				continue
			}
			return nil, fmt.Errorf("layout node is not in graph")
		}
		if math.IsNaN(pos.X) || math.IsNaN(pos.Y) || math.IsInf(pos.X, 0) || math.IsInf(pos.Y, 0) || math.Abs(pos.X) > 100000 || math.Abs(pos.Y) > 100000 {
			return nil, fmt.Errorf("invalid node position")
		}
	}
	return json.Marshal(layout)
}
