package jsonutil

import (
	"encoding/json"
	"testing"
)

func TestPathLookup(t *testing.T) {
	data := `{"data":{"items":[{"id":"a","customer.id":"c-1"},{"id":"b"}],"name":"Alice","none":null,"nums":[1,2,3]}}`
	cases := []struct {
		path  string
		want  string
		found bool
	}{
		{"$.data.name", `"Alice"`, true},
		{".data.name", `"Alice"`, true},
		{"data.name", `"Alice"`, true},
		{"data.items[0].id", `"a"`, true},
		{"data.items[1].id", `"b"`, true},
		{`data.items[0]["customer.id"]`, `"c-1"`, true},
		{"data.nums[2]", `3`, true},
		{"data.none", `null`, true},
		{"", `<root>`, true},
		{"$", `<root>`, true},
		{"data.missing", ``, false},
		{"data.items[5]", ``, false},
		{"data.items[0].missing", ``, false},
		{"data.name.length", ``, false},
		{"data.items[x]", ``, false},
		{"data.items[0", ``, false},
		{"data.name.", ``, false},
		{"data..name", ``, false},
		{`data["name"]`, `"Alice"`, true},
		{`data["unterminated`, ``, false},
	}
	for _, item := range cases {
		var root any
		if err := Unmarshal([]byte(data), &root); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		value, ok := PathLookup(root, item.path)
		if ok != item.found {
			t.Fatalf("path %q: found=%v want %v", item.path, ok, item.found)
		}
		if !item.found {
			continue
		}
		if item.want == "<root>" {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal result: %v", err)
		}
		if string(encoded) != item.want {
			t.Fatalf("path %q: got %s want %s", item.path, encoded, item.want)
		}
	}
}

func TestPathSegments(t *testing.T) {
	valid := map[string]int{
		"trigger.customerId":          2,
		"s1.data.items[0].id":         5,
		`s1.data["customer.id"].name`: 4,
		"s1[0]":                       2,
	}
	for path, want := range valid {
		segments, ok := PathSegments(path)
		if !ok {
			t.Fatalf("path %q expected valid", path)
		}
		if len(segments) != want {
			t.Fatalf("path %q: got %d segments, want %d", path, len(segments), want)
		}
	}
	invalid := []string{"", "$", ".", "s1.", "s1..a", "s1[0]a", `s1["a`, "s1[-1]", "s1[a]", "s1[]"}
	for _, path := range invalid {
		if _, ok := PathSegments(path); ok {
			t.Fatalf("path %q expected invalid", path)
		}
	}
}
