// Package safejson renders JSON previews with sensitive fields redacted and a
// hard size cap. It backs operation previews, audit before/after values and
// workflow step previews so every persistence path shares one redactor.
package safejson

import (
	"encoding/json"
	"strings"

	"apihub-go/internal/jsonutil"
)

// Marshal encodes value, redacts sensitive keys and returns at most limit
// bytes; oversized payloads collapse to a truncation marker instead of a
// partial object.
func Marshal(value any, limit int) []byte {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var normalized any
	if jsonutil.Unmarshal(data, &normalized) == nil {
		data, err = json.Marshal(Redact(normalized))
		if err != nil {
			return nil
		}
	}
	if len(data) > limit {
		return []byte(`{"truncated":true}`)
	}
	return data
}

// Redact recursively replaces values under sensitive field names.
func Redact(value any) any {
	switch current := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(current))
		for key, item := range current {
			if SensitiveKey(key) {
				redacted[key] = "[REDACTED]"
				continue
			}
			redacted[key] = Redact(item)
		}
		return redacted
	case []any:
		redacted := make([]any, len(current))
		for index, item := range current {
			redacted[index] = Redact(item)
		}
		return redacted
	default:
		return current
	}
}

// SensitiveKey reports whether a field name looks like a credential. Key-name
// matching is a preview safeguard only; it cannot recognize arbitrary secret
// values stored under innocent names.
func SensitiveKey(key string) bool {
	normalized := strings.Map(func(char rune) rune {
		if char >= 'A' && char <= 'Z' {
			return char + ('a' - 'A')
		}
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			return char
		}
		return -1
	}, key)
	if normalized == "authorization" || normalized == "proxyauthorization" ||
		normalized == "cookie" || normalized == "setcookie" ||
		normalized == "apikey" || normalized == "accesskey" || normalized == "privatekey" {
		return true
	}
	return strings.HasSuffix(normalized, "password") ||
		strings.HasSuffix(normalized, "passwd") ||
		strings.HasSuffix(normalized, "secret") ||
		strings.HasSuffix(normalized, "token")
}
