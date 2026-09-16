package executor

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func TestPathExpansionRejectsTraversal(t *testing.T) {
	for _, id := range []any{"..", ".", "../admin", "a/b", `a\b`, "%2e%2e", "%252e%252e", nil, map[string]any{"id": 1}} {
		if _, _, err := expandPath("/allowed/{id}/items", map[string]any{"id": id}); err == nil {
			t.Fatalf("accepted unsafe value %#v", id)
		}
	}
	path, rest, err := expandPath("/allowed/{id}/{id}", map[string]any{"id": json.Number("9007199254740993"), "page": json.Number("1")})
	if err != nil || path != "/allowed/9007199254740993/9007199254740993" || len(rest) != 1 {
		t.Fatalf("path %s rest %v err %v", path, rest, err)
	}
	for _, path := range []string{"//evil.test/x", "/a/../x", "/a/%2e%2e/x", "/a/%252e%252e/x", "https://evil.test/x", "/x?y=1", "/x#f"} {
		if ValidatePath(path) == nil {
			t.Fatalf("accepted %s", path)
		}
	}
}
func TestRequestedRedirectCompatibility(t *testing.T) {
	previous, _ := url.Parse("https://example.test/path")
	next, _ := url.Parse("http://example.test/path")
	client := NewGuardedClient()
	if err := client.CheckRedirect(&http.Request{URL: next}, []*http.Request{{URL: previous}}); err != nil {
		t.Fatalf("user-excluded redirect behavior changed: %v", err)
	}
}
