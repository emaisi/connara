package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"apihub-go/internal/jsonutil"
)

func parseOrDie(t *testing.T, document string) Definition {
	t.Helper()
	definition, err := ParseDefinition([]byte(document))
	if err != nil {
		t.Fatalf("parse definition: %v", err)
	}
	return definition
}

func TestValidateRejectsReservedAndDuplicateAliases(t *testing.T) {
	base := `{"steps":[{"id":"%s","action":"a.b"}],"output":{}}`
	for _, alias := range []string{"trigger", "status", "", "has space", "dotted.id", strings.Repeat("x", 65)} {
		if err := ValidateDefinition(parseOrDie(t, strings.Replace(base, "%s", alias, 1)), false, nil); err == nil {
			t.Fatalf("alias %q expected rejection", alias)
		}
	}
	duplicate := `{"steps":[{"id":"a","action":"x.y"},{"id":"a","action":"x.z"}],"output":{}}`
	if err := ValidateDefinition(parseOrDie(t, duplicate), false, nil); err == nil {
		t.Fatal("duplicate alias expected rejection")
	}
}

func TestValidateRejectsUnknownAndCyclicDependencies(t *testing.T) {
	unknown := `{"steps":[{"id":"a","action":"x.y","dependsOn":["ghost"]}],"output":{}}`
	if err := ValidateDefinition(parseOrDie(t, unknown), false, nil); err == nil {
		t.Fatal("unknown dependency expected rejection")
	}
	cycle := `{"steps":[{"id":"a","action":"x.y","dependsOn":["b"]},{"id":"b","action":"x.z","dependsOn":["a"]}],"output":{}}`
	if err := ValidateDefinition(parseOrDie(t, cycle), false, nil); err == nil {
		t.Fatal("cycle expected rejection")
	}
	self := `{"steps":[{"id":"a","action":"x.y","dependsOn":["a"]}],"output":{}}`
	if err := ValidateDefinition(parseOrDie(t, self), false, nil); err == nil {
		t.Fatal("self dependency expected rejection")
	}
}

func TestValidateEnforcesReferenceClosure(t *testing.T) {
	document := `{
		"steps":[
			{"id":"s1","action":"a.one","input":{"x":"{{trigger.x}}"}},
			{"id":"s2","action":"a.two","input":{"y":"{{s3.data}}"},"dependsOn":["s1"]}
		],
		"output":{"o":"{{s2.data.id}}"}
	}`
	err := ValidateDefinition(parseOrDie(t, document), false, nil)
	if err == nil || !strings.Contains(err.Error(), "outside of its dependsOn closure") {
		t.Fatalf("expected closure violation, got %v", err)
	}
	// Referencing an undeclared alias in output is also rejected.
	document = `{"steps":[{"id":"s1","action":"a.one"}],"output":{"o":"{{ghost.data}}"}}`
	if err := ValidateDefinition(parseOrDie(t, document), false, nil); err == nil {
		t.Fatal("output reference to undeclared alias expected rejection")
	}
}

func TestValidateAcceptsTransitiveClosure(t *testing.T) {
	document := `{
		"steps":[
			{"id":"s1","action":"a.one","input":{"x":"{{trigger.x}}"}},
			{"id":"s2","action":"a.two","input":{"y":"{{s1.data}}"},"dependsOn":["s1"]},
			{"id":"s3","action":"a.three","input":{"z":"{{s1.meta}}","w":"{{status.s2}}"},"dependsOn":["s2"]}
		],
		"output":{"o":"{{s3.data}}","t":"{{trigger.x}}"}
	}`
	if err := ValidateDefinition(parseOrDie(t, document), false, nil); err != nil {
		t.Fatalf("valid definition rejected: %v", err)
	}
}

func TestValidateRequiresExplicitBindingsOnDeploy(t *testing.T) {
	document := `{"steps":[{"id":"s1","action":"a.one","input":{}}],"output":{"o":1}}`
	if err := ValidateDefinition(parseOrDie(t, document), true, nil); err == nil {
		t.Fatal("deploy validation without integration binding expected rejection")
	}
	empty := `{"steps":[],"output":{}}`
	if err := ValidateDefinition(parseOrDie(t, empty), true, nil); err == nil {
		t.Fatal("deploy validation without steps expected rejection")
	}
	noOutput := `{"steps":[{"id":"s1","action":"a.one","integrationId":"i","connectionKey":"c"}],"output":{}}`
	if err := ValidateDefinition(parseOrDie(t, noOutput), true, nil); err == nil {
		t.Fatal("deploy validation without output mapping expected rejection")
	}
}

func TestValidateRejectsBadConditionsAndOnError(t *testing.T) {
	cases := map[string]string{
		"unknown op": `{"steps":[{"id":"s1","action":"a.one","runIf":{"path":"trigger.x","op":"regex","value":1}}],"output":{}}`,
		"deep nest":  `{"steps":[{"id":"s1","action":"a.one","runIf":{"all":[{"all":[{"all":[{"path":"trigger.x","op":"exists"}]}]}]}}],"output":{}}`,
		"mixed":      `{"steps":[{"id":"s1","action":"a.one","runIf":{"path":"trigger.x","op":"eq","value":1,"all":[{"path":"trigger.y","op":"exists"}]}}],"output":{}}`,
		"bad path":   `{"steps":[{"id":"s1","action":"a.one","runIf":{"path":"trigger..x","op":"exists"}}],"output":{}}`,
		"ordering":   `{"steps":[{"id":"s1","action":"a.one","runIf":{"path":"trigger.x","op":"gt","value":"1"}}],"output":{}}`,
		"on error":   `{"steps":[{"id":"s1","action":"a.one","onError":"skip"}],"output":{}}`,
		"too many": func() string {
			leaves := make([]string, 0, 9)
			for i := 0; i < 9; i++ {
				leaves = append(leaves, `{"path":"trigger.x","op":"exists"}`)
			}
			return `{"steps":[{"id":"s1","action":"a.one","runIf":{"all":[` + strings.Join(leaves, ",") + `]}}],"output":{}}`
		}(),
	}
	for name, document := range cases {
		if err := ValidateDefinition(parseOrDie(t, document), false, nil); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

func TestOrderStepsStableTopologicalOrder(t *testing.T) {
	document := `{
		"steps":[
			{"id":"c","action":"a.c","dependsOn":["a","b"]},
			{"id":"a","action":"a.a"},
			{"id":"b","action":"a.b","dependsOn":["a"]}
		],
		"output":{}
	}`
	definition := parseOrDie(t, document)
	order, err := OrderSteps(definition.Steps)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	var sequence []string
	for _, index := range order {
		sequence = append(sequence, definition.Steps[index].ID)
	}
	if strings.Join(sequence, ",") != "a,b,c" {
		t.Fatalf("expected a,b,c got %v", sequence)
	}
}

func TestResolvePreservesTypesAndInterpolates(t *testing.T) {
	var context any
	if err := jsonutil.Unmarshal([]byte(`{
		"trigger":{"customerId":"C-1","count":3,"ratio":2.5,"flag":true},
		"s1":{"data":{"items":[{"id":"i0"},{"id":"i1"}],"customer.id":"c-9","name":"Alice"}}
	}`), &context); err != nil {
		t.Fatalf("decode context: %v", err)
	}
	value, err := Resolve(context.(map[string]any), map[string]any{
		"whole":     "{{trigger.count}}",
		"wholeRef":  "{{s1}}",
		"item":      "{{s1.data.items[1].id}}",
		"mixed":     "customer {{s1.data.name}} #{{trigger.count}}",
		"nested":    map[string]any{"deep": []any{"{{trigger.flag}}"}},
		"quoted":    `{{s1.data["customer.id"]}}`,
		"plain":     "unchanged",
		"numberObj": 42,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	object := value.(map[string]any)
	if number, ok := object["whole"].(json.Number); !ok || number.String() != "3" {
		t.Fatalf("whole placeholder must keep json.Number, got %#v", object["whole"])
	}
	if _, ok := object["wholeRef"].(map[string]any); !ok {
		t.Fatalf("whole object reference must keep object type, got %#v", object["wholeRef"])
	}
	if object["item"] != "i1" {
		t.Fatalf("array index reference failed: %#v", object["item"])
	}
	if object["mixed"] != "customer Alice #3" {
		t.Fatalf("mixed interpolation failed: %#v", object["mixed"])
	}
	if object["quoted"] != "c-9" {
		t.Fatalf("quoted field reference failed: %#v", object["quoted"])
	}
	if object["plain"] != "unchanged" || object["numberObj"] != 42 {
		t.Fatal("plain values must pass through")
	}
	nested := object["nested"].(map[string]any)
	if nested["deep"].([]any)[0] != true {
		t.Fatalf("nested boolean interpolation failed: %#v", nested)
	}
}

func TestResolveMissingPathAndNullSemantics(t *testing.T) {
	context := map[string]any{"trigger": map[string]any{"none": nil, "text": "x"}}
	if _, err := Resolve(context, map[string]any{"a": "{{trigger.missing}}"}); err == nil {
		t.Fatal("missing path expected error")
	}
	value, err := Resolve(context, map[string]any{"a": "{{trigger.none}}"})
	if err != nil {
		t.Fatalf("null path must resolve: %v", err)
	}
	if value.(map[string]any)["a"] != nil {
		t.Fatal("null must resolve to nil")
	}
	if _, err := Resolve(context, map[string]any{"a": "v={{trigger.none}}"}); err == nil {
		t.Fatal("null in mixed text expected error")
	}
	if _, err := Resolve(context, map[string]any{"a": "{{trigger}}"}); err != nil {
		t.Fatalf("whole trigger object reference allowed: %v", err)
	}
	if _, err := Resolve(context, map[string]any{"a": "v={{trigger}}"}); err == nil {
		t.Fatal("object in mixed text expected error")
	}
}

func TestResolveOutputOptionalReferences(t *testing.T) {
	context := map[string]any{"trigger": map[string]any{}}
	value, err := ResolveOutput(context, map[string]any{"maybe": "{{?s1.data.orderId}}"})
	if err != nil {
		t.Fatalf("optional reference: %v", err)
	}
	if value.(map[string]any)["maybe"] != nil {
		t.Fatal("optional missing reference must be nil")
	}
	if _, err := Resolve(context, map[string]any{"a": "{{?s1.data}}"}); err == nil {
		t.Fatal("optional reference outside output expected error")
	}
	if _, err := ResolveOutput(context, map[string]any{"a": "x{{?s1.data}}"}); err == nil {
		t.Fatal("optional reference in mixed text expected error")
	}
}

func TestEvaluateConditions(t *testing.T) {
	context := map[string]any{}
	if err := jsonutil.Unmarshal([]byte(`{
		"trigger":{"vip":true,"score":42,"missing":null,"name":"Alice"},
		"s1":{"data":{"count":5}},
		"status":{"s1":"success","s2":"skipped"}
	}`), &context); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cases := []struct {
		name      string
		condition string
		want      bool
		wantError bool
	}{
		{"eq number", `{"path":"s1.data.count","op":"eq","value":5}`, true, false},
		{"eq number string form", `{"path":"s1.data.count","op":"eq","value":"5"}`, false, false},
		{"ne deep", `{"path":"trigger","op":"ne","value":{"vip":true}}`, true, false},
		{"eq deep object", `{"path":"trigger","op":"eq","value":{"vip":true,"score":42,"missing":null,"name":"Alice"}}`, true, false},
		{"gt", `{"path":"s1.data.count","op":"gt","value":4}`, true, false},
		{"gte false", `{"path":"s1.data.count","op":"gte","value":6}`, false, false},
		{"lt missing", `{"path":"s1.data.absent","op":"lt","value":1}`, false, true},
		{"exists null", `{"path":"trigger.missing","op":"exists"}`, true, false},
		{"not_exists", `{"path":"s1.data.absent","op":"not_exists"}`, true, false},
		{"status eq", `{"path":"status.s1","op":"eq","value":"success"}`, true, false},
		{"all short-circuit", `{"all":[{"path":"status.s2","op":"eq","value":"success"},{"path":"s1.data.missing","op":"eq","value":1}]}`, false, false},
		{"any", `{"any":[{"path":"trigger.vip","op":"eq","value":false},{"path":"s1.data.count","op":"eq","value":5}]}`, true, false},
		{"nested group", `{"all":[{"any":[{"path":"trigger.vip","op":"eq","value":true}]},{"path":"status.s1","op":"eq","value":"success"}]}`, true, false},
		{"gt non numeric left", `{"path":"trigger.name","op":"gt","value":1}`, false, true},
	}
	for _, item := range cases {
		var condition Condition
		if err := jsonutil.Unmarshal([]byte(item.condition), &condition); err != nil {
			t.Fatalf("%s: decode: %v", item.name, err)
		}
		got, err := Evaluate(context, &condition)
		if item.wantError {
			if err == nil {
				t.Fatalf("%s: expected error", item.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", item.name, err)
		}
		if got != item.want {
			t.Fatalf("%s: got %v want %v", item.name, got, item.want)
		}
	}
}

func TestValidateConditionGroupShape(t *testing.T) {
	var empty Condition
	if err := jsonutil.Unmarshal([]byte(`{"all":[]}`), &empty); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCondition(&empty); err == nil {
		t.Fatal("empty group expected rejection")
	}
	var both Condition
	if err := jsonutil.Unmarshal([]byte(`{"all":[{"path":"a","op":"exists"}],"any":[{"path":"b","op":"exists"}]}`), &both); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCondition(&both); err == nil {
		t.Fatal("all and any together expected rejection")
	}
}

func TestParseDefinitionNumberPreservation(t *testing.T) {
	definition := parseOrDie(t, `{"steps":[{"id":"s","action":"a.b","input":{"n":"{{trigger.big}}"}}],"output":{"o":"{{s.data}}"}}`)
	if len(definition.Steps) != 1 || definition.Steps[0].Action != "a.b" {
		t.Fatalf("unexpected definition: %#v", definition)
	}
}
