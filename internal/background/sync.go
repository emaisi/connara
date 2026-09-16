package background

import (
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/model"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type SyncConfig struct {
	RecordsPath   string `json:"recordsPath"`
	IDPath        string `json:"idPath"`
	CursorPath    string `json:"cursorPath"`
	CursorParam   string `json:"cursorParam"`
	PageSizeParam string `json:"pageSizeParam"`
	PageSize      int    `json:"pageSize"`
	MaxPages      int    `json:"maxPages"`
}

func ParseSyncConfig(data []byte) (SyncConfig, error) {
	var c SyncConfig
	if len(data) > 0 {
		if err := jsonutil.Unmarshal(data, &c); err != nil {
			return c, err
		}
	}
	if c.IDPath == "" {
		c.IDPath = "id"
	}
	if c.MaxPages == 0 {
		c.MaxPages = 100
	}
	if c.MaxPages < 1 || c.MaxPages > 10000 || c.PageSize < 0 || c.PageSize > 1000 {
		return c, errors.New("invalid sync page limits")
	}
	if (c.CursorPath == "") != (c.CursorParam == "") {
		return c, errors.New("cursorPath and cursorParam must be set together")
	}
	return c, nil
}
func lookup(value any, path string) (any, bool) {
	path = strings.TrimPrefix(strings.TrimPrefix(path, "$"), ".")
	if path == "" {
		return value, true
	}
	for _, key := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[key]
		if !ok {
			return nil, false
		}
	}
	return value, true
}
func scalar(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, v != ""
	case json.Number:
		return string(v), true
	default:
		return "", false
	}
}
func extractSyncPage(taskID string, data []byte, c SyncConfig) ([]model.SyncRecord, string, error) {
	var root any
	if err := jsonutil.Unmarshal(data, &root); err != nil {
		return nil, "", errors.New("sync response must be JSON")
	}
	value := root
	if c.RecordsPath != "" {
		var ok bool
		value, ok = lookup(root, c.RecordsPath)
		if !ok {
			return nil, "", errors.New("records path is missing")
		}
	} else if object, ok := value.(map[string]any); ok {
		if nested, exists := object["data"]; exists {
			value = nested
		}
	}
	items, ok := value.([]any)
	if !ok {
		items = []any{value}
	}
	records := make([]model.SyncRecord, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		value, exists := lookup(item, c.IDPath)
		id, valid := scalar(value)
		if !exists || !valid {
			return nil, "", fmt.Errorf("record is missing stable scalar ID at %s", c.IDPath)
		}
		if seen[id] {
			return nil, "", fmt.Errorf("duplicate external ID %s in page", id)
		}
		seen[id] = true
		payload, err := json.Marshal(item)
		if err != nil {
			return nil, "", err
		}
		records = append(records, model.SyncRecord{SyncTaskID: taskID, Model: "record", ExternalID: id, Payload: payload})
	}
	cursor := ""
	if c.CursorPath != "" {
		value, _ := lookup(root, c.CursorPath)
		if value != nil && value != "" {
			var valid bool
			cursor, valid = scalar(value)
			if !valid {
				return nil, "", errors.New("next cursor must be a string or number")
			}
		}
	}
	return records, cursor, nil
}
func syncRecords(taskID string, data []byte) ([]model.SyncRecord, error) {
	c, _ := ParseSyncConfig(nil)
	records, _, err := extractSyncPage(taskID, data, c)
	return records, err
}
