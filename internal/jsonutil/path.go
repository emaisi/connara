package jsonutil

import (
	"strconv"
	"strings"
)

// PathLookup resolves a dotted JSON path against a decoded value. Besides the
// plain key segments used by sync pagination it accepts [3] array indexes and
// ["key.with.dots"] quoted field names. The legacy "$" and leading "."
// prefixes of the sync configuration stay supported.
func PathLookup(value any, path string) (any, bool) {
	rest := strings.TrimPrefix(strings.TrimPrefix(path, "$"), ".")
	if rest == "" {
		return value, true
	}
	current := value
	for {
		var ok bool
		current, rest, ok = stepPath(current, rest)
		if !ok {
			return nil, false
		}
		if rest == "" {
			return current, true
		}
		rest = strings.TrimPrefix(rest, ".")
		if rest == "" {
			// A trailing dot never matched a field in the previous parser.
			return nil, false
		}
	}
}

func stepPath(value any, rest string) (any, string, bool) {
	if strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end < 1 {
			return nil, "", false
		}
		token := rest[1:end]
		tail := rest[end+1:]
		if len(token) >= 2 && token[0] == '"' && token[len(token)-1] == '"' {
			key := token[1 : len(token)-1]
			if key == "" || strings.ContainsAny(key, "\"\\]") {
				return nil, "", false
			}
			object, ok := value.(map[string]any)
			if !ok {
				return nil, "", false
			}
			next, exists := object[key]
			return next, tail, exists
		}
		index, err := strconv.Atoi(strings.TrimSpace(token))
		if err != nil || index < 0 {
			return nil, "", false
		}
		items, ok := value.([]any)
		if !ok || index >= len(items) {
			return nil, "", false
		}
		return items[index], tail, true
	}
	name := rest
	tail := ""
	if position := strings.IndexAny(rest, ".["); position >= 0 {
		name = rest[:position]
		tail = rest[position:]
	}
	if name == "" {
		return nil, "", false
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, "", false
	}
	next, exists := object[name]
	return next, tail, exists
}

// PathSegments splits a path into its accessors: map keys and non-negative
// array indexes. It rejects the same malformed syntax PathLookup ignores so
// workflow definitions can fail at save time instead of run time.
func PathSegments(path string) ([]PathSegment, bool) {
	rest := strings.TrimPrefix(strings.TrimPrefix(path, "$"), ".")
	if rest == "" {
		return nil, false
	}
	var segments []PathSegment
	for rest != "" {
		if strings.HasPrefix(rest, "[") {
			end := strings.IndexByte(rest, ']')
			if end < 1 {
				return nil, false
			}
			token := rest[1:end]
			rest = rest[end+1:]
			if len(token) >= 2 && token[0] == '"' && token[len(token)-1] == '"' {
				key := token[1 : len(token)-1]
				if key == "" || strings.ContainsAny(key, "\"\\]") {
					return nil, false
				}
				segments = append(segments, PathSegment{Key: key})
			} else {
				index, err := strconv.Atoi(strings.TrimSpace(token))
				if err != nil || index < 0 {
					return nil, false
				}
				segments = append(segments, PathSegment{Index: index, IsIndex: true})
			}
			if rest != "" && !strings.HasPrefix(rest, ".") && !strings.HasPrefix(rest, "[") {
				return nil, false
			}
		} else {
			name := rest
			if position := strings.IndexAny(rest, ".["); position >= 0 {
				name = rest[:position]
				rest = rest[position:]
			} else {
				rest = ""
			}
			if name == "" {
				return nil, false
			}
			segments = append(segments, PathSegment{Key: name})
		}
		if strings.HasPrefix(rest, ".") {
			rest = rest[1:]
			if rest == "" {
				return nil, false
			}
		}
	}
	return segments, true
}

// PathSegment is one accessor of a dotted path: either a map key or an array
// index.
type PathSegment struct {
	Key     string
	Index   int
	IsIndex bool
}

// ValidPath checks supported read syntax independently of the current document.
func ValidPath(path string) bool {
	if path == "$" {
		return true
	}
	if !strings.HasPrefix(path, "$.") && !strings.HasPrefix(path, "$[") {
		return false
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(path, "$"), ".")
	for rest != "" {
		if rest[0] == '[' {
			end := strings.IndexByte(rest, ']')
			if end < 1 {
				return false
			}
			token := rest[1:end]
			if token[0] == '"' {
				if len(token) < 3 || token[len(token)-1] != '"' || strings.ContainsAny(token[1:len(token)-1], "\"\\]") {
					return false
				}
			} else {
				if i, err := strconv.Atoi(token); err != nil || i < 0 {
					return false
				}
			}
			rest = rest[end+1:]
		} else {
			end := strings.IndexAny(rest, ".[")
			if end < 0 {
				return !strings.ContainsAny(rest, " ]$")
			}
			if end == 0 {
				return false
			}
			if strings.ContainsAny(rest[:end], " ]$") {
				return false
			}
			rest = rest[end:]
		}
		if strings.HasPrefix(rest, ".") {
			rest = rest[1:]
			if rest == "" || rest[0] == '[' {
				return false
			}
		} else if rest != "" && rest[0] != '[' {
			return false
		}
	}
	return true
}
