package sdkcontract

import (
	"os"
	"strings"
	"testing"
)

const checkerSpec = `
paths:
  /things/{id}:
    get:
      operationId: things.get
      responses:
        default: {description: e, content: {application/json: {schema: {$ref: '#/components/schemas/Err'}}}}
        '200': {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}}
  /things/special:
    get:
      operationId: things.special
      responses:
        '204': {description: empty}
components:
  schemas:
    Err: {type: object, required: [error], properties: {error: {type: string}, message: {type: string}}}
    Thing:
      type: object
      required: [id, count]
      properties:
        id: {type: string}
        count: {type: integer}
        when: {anyOf: [{type: string, format: date-time}, {type: 'null'}]}
        kind: {type: string, enum: [a, b]}
        tags: {anyOf: [{type: array, items: {type: string}}, {type: 'null'}]}
        extra: {type: object, additionalProperties: {type: string}}
        opaque: {description: anything}
`

func TestResponseCheckerIsStrictWhereTheContractIsClosed(t *testing.T) {
	c, err := NewResponseChecker([]byte(checkerSpec))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, path string
		status             int
		body, wantErr, op  string
	}{
		{"valid", "GET", "/things/x", 200, `{"id":"x","count":2,"when":null,"kind":"a","tags":["t"],"extra":{"k":"v"},"opaque":{"any":[1]}}`, "", "things.get"},
		{"undeclared property", "GET", "/things/x", 200, `{"id":"x","count":2,"surprise":true}`, `property "surprise" is not declared`, "things.get"},
		{"missing required", "GET", "/things/x", 200, `{"id":"x"}`, `required property "count" is missing`, "things.get"},
		{"integer is not number", "GET", "/things/x", 200, `{"id":"x","count":1.5}`, "integer expected", "things.get"},
		{"wrong type", "GET", "/things/x", 200, `{"id":3,"count":1}`, "string expected", "things.get"},
		{"bad date-time", "GET", "/things/x", 200, `{"id":"x","count":1,"when":"yesterday"}`, "anyOf", "things.get"},
		{"enum", "GET", "/things/x", 200, `{"id":"x","count":1,"kind":"c"}`, "not in enum", "things.get"},
		{"additional properties schema", "GET", "/things/x", 200, `{"id":"x","count":1,"extra":{"k":1}}`, "string expected", "things.get"},
		{"undeclared success status", "GET", "/things/x", 201, `{"id":"x","count":1}`, "status 201 is not declared", "things.get"},
		{"error through default", "GET", "/things/x", 404, `{"error":"Not Found","message":"nope"}`, "", "things.get"},
		{"error shape", "GET", "/things/x", 500, `{"message":"no error field"}`, `required property "error"`, "things.get"},
		{"literal segment wins", "GET", "/things/special", 204, ``, "", "things.special"},
		{"204 with a body", "GET", "/things/special", 204, `{}`, "declares no body", "things.special"},
		{"no operation", "POST", "/things/x", 200, `{}`, "no contract operation", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op, err := c.Check(tc.method, tc.path, tc.status, []byte(tc.body))
			if op != tc.op {
				t.Errorf("operation %q, want %q", op, tc.op)
			}
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("err=%v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestResponseCheckerLoadsTheRepositoryContract(t *testing.T) {
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewResponseChecker(data)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"/tasks/p/features/f/runner-candidates": "features.runnerCandidates",
		"/tasks/p/t/runner-candidates":          "tasks.runnerCandidates",
		"/tasks/p/features/ready":               "features.ready",
		"/entries/projects%2Fp%2Fnote%2Fa.md":   "entries.get",
		"/supervision/operations/op-1":          "supervision.getOperation",
	} {
		if got := c.Operation("GET", path); got != want {
			t.Errorf("GET %s -> %q, want %q", path, got, want)
		}
	}
}
