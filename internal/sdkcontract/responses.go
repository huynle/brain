package sdkcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ResponseChecker checks actual HTTP responses against the response the
// public contract declares for their operation and status. It is strict where
// the contract is closed: an undeclared object property fails unless the
// schema allows additionalProperties, required properties must be present,
// JSON types must match (integer is not number), enums and date-times are
// checked, and a status the operation does not declare (nor covers with
// default) fails. It is a test aid for live handler responses, not a general
// JSON Schema validator: unsupported keywords are ignored.
type ResponseChecker struct {
	schemas map[string]any
	routes  []checkedRoute
}

type checkedRoute struct {
	id, method, template string
	literals             int
	pattern              *regexp.Regexp
	responses            map[string]any
}

// NewResponseChecker indexes every operation in an OpenAPI document.
func NewResponseChecker(openapi []byte) (*ResponseChecker, error) {
	var doc struct {
		Paths      map[string]map[string]any `yaml:"paths"`
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(openapi, &doc); err != nil {
		return nil, err
	}
	c := &ResponseChecker{schemas: doc.Components.Schemas}
	for template, item := range doc.Paths {
		var expr strings.Builder
		literals := 0
		for _, segment := range strings.Split(strings.Trim(template, "/"), "/") {
			expr.WriteString("/")
			if strings.HasPrefix(segment, "{") {
				expr.WriteString("[^/]+")
				continue
			}
			literals++
			expr.WriteString(regexp.QuoteMeta(segment))
		}
		pattern := regexp.MustCompile("^" + expr.String() + "$")
		for method, raw := range item {
			op, ok := raw.(map[string]any)
			if !ok || method == "parameters" {
				continue
			}
			id, _ := op["operationId"].(string)
			responses, _ := op["responses"].(map[string]any)
			c.routes = append(c.routes, checkedRoute{id, strings.ToUpper(method), template, literals, pattern, responses})
		}
	}
	// Literal segments win, as they do in the router.
	sort.Slice(c.routes, func(i, j int) bool {
		if c.routes[i].literals != c.routes[j].literals {
			return c.routes[i].literals > c.routes[j].literals
		}
		return c.routes[i].template < c.routes[j].template
	})
	return c, nil
}

// Operation returns the operation that serves method and path (the escaped
// path below /api/v1), or "" when the contract has none.
func (c *ResponseChecker) Operation(method, path string) string {
	if r := c.route(method, path); r != nil {
		return r.id
	}
	return ""
}

func (c *ResponseChecker) route(method, path string) *checkedRoute {
	for i := range c.routes {
		if c.routes[i].method == method && c.routes[i].pattern.MatchString(path) {
			return &c.routes[i]
		}
	}
	return nil
}

// Check validates one response and returns the operation it belongs to.
func (c *ResponseChecker) Check(method, path string, status int, body []byte) (string, error) {
	r := c.route(method, path)
	if r == nil {
		return "", fmt.Errorf("%s %s: no contract operation", method, path)
	}
	declared, ok := r.responses[strconv.Itoa(status)]
	if !ok {
		declared, ok = r.responses["default"]
		if !ok || (status >= 200 && status < 300) {
			// default documents errors; a success status must be declared.
			return r.id, fmt.Errorf("%s: status %d is not declared", r.id, status)
		}
	}
	response, _ := declared.(map[string]any)
	content, _ := response["content"].(map[string]any)
	media, _ := content["application/json"].(map[string]any)
	if media == nil {
		if len(bytes.TrimSpace(body)) != 0 && content == nil {
			return r.id, fmt.Errorf("%s: status %d declares no body but one was sent", r.id, status)
		}
		return r.id, nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return r.id, fmt.Errorf("%s: status %d body is not JSON: %v", r.id, status, err)
	}
	if err := c.validate(media["schema"], value, "$"); err != nil {
		return r.id, fmt.Errorf("%s: status %d: %v", r.id, status, err)
	}
	return r.id, nil
}

func (c *ResponseChecker) resolve(schema any) map[string]any {
	s, _ := schema.(map[string]any)
	for s != nil {
		ref, ok := s["$ref"].(string)
		if !ok {
			break
		}
		s, _ = c.schemas[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
	}
	return s
}

func (c *ResponseChecker) validate(schema, value any, at string) error {
	s := c.resolve(schema)
	if s == nil {
		return nil
	}
	if branches, ok := s["anyOf"].([]any); ok {
		var errs []string
		for _, b := range branches {
			err := c.validate(b, value, at)
			if err == nil {
				return nil
			}
			errs = append(errs, err.Error())
		}
		return fmt.Errorf("%s matches no anyOf branch: %s", at, strings.Join(errs, "; "))
	}
	if branches, ok := s["allOf"].([]any); ok {
		merged := map[string]any{"type": "object", "properties": map[string]any{}}
		var required []any
		for _, b := range branches {
			bs := c.resolve(b)
			for k, v := range mapOf(bs["properties"]) {
				merged["properties"].(map[string]any)[k] = v
			}
			if r, ok := bs["required"].([]any); ok {
				required = append(required, r...)
			}
		}
		merged["required"] = required
		return c.validate(merged, value, at)
	}
	if enum, ok := s["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if fmt.Sprint(e) == fmt.Sprint(value) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%s: %v is not in enum %v", at, value, enum)
		}
	}
	switch want := s["type"].(type) {
	case string:
		if err := checkType(want, value, at); err != nil {
			return err
		}
	case []any:
		matched := false
		for _, w := range want {
			if checkType(fmt.Sprint(w), value, at) == nil {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: one of %v expected, got %s", at, want, kindOf(value))
		}
	}
	if s["format"] == "date-time" {
		if str, ok := value.(string); ok {
			if _, err := time.Parse(time.RFC3339Nano, str); err != nil {
				return fmt.Errorf("%s: %q is not an RFC 3339 date-time", at, str)
			}
		}
	}
	_, hasProps := s["properties"]
	_, hasAdditional := s["additionalProperties"]
	describesObject := s["type"] == "object" || hasProps || hasAdditional
	switch v := value.(type) {
	case map[string]any:
		if !describesObject {
			break // an unconstrained schema ({}) accepts any object
		}
		props := mapOf(s["properties"])
		for _, r := range sliceOf(s["required"]) {
			if _, ok := v[fmt.Sprint(r)]; !ok {
				return fmt.Errorf("%s: required property %q is missing", at, r)
			}
		}
		additional := s["additionalProperties"]
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if p, ok := props[k]; ok {
				if err := c.validate(p, v[k], at+"."+k); err != nil {
					return err
				}
				continue
			}
			switch a := additional.(type) {
			case bool:
				if !a {
					return fmt.Errorf("%s: property %q is not declared", at, k)
				}
			case map[string]any:
				if err := c.validate(a, v[k], at+"."+k); err != nil {
					return err
				}
			default:
				if !hasAdditional {
					return fmt.Errorf("%s: property %q is not declared", at, k)
				}
			}
		}
	case []any:
		if items, ok := s["items"]; ok {
			for i, item := range v {
				if err := c.validate(items, item, fmt.Sprintf("%s[%d]", at, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkType(want string, value any, at string) error {
	ok := false
	switch want {
	case "null":
		ok = value == nil
	case "boolean":
		_, ok = value.(bool)
	case "string":
		_, ok = value.(string)
	case "object":
		_, ok = value.(map[string]any)
	case "array":
		_, ok = value.([]any)
	case "number":
		_, ok = value.(json.Number)
	case "integer":
		if n, isNumber := value.(json.Number); isNumber {
			_, err := strconv.ParseInt(n.String(), 10, 64)
			ok = err == nil
		}
	default:
		ok = true
	}
	if !ok {
		return fmt.Errorf("%s: %s expected, got %s", at, want, kindOf(value))
	}
	return nil
}

func kindOf(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case json.Number:
		return "number " + v.String()
	default:
		return fmt.Sprintf("%T", v)
	}
}

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func sliceOf(v any) []any {
	s, _ := v.([]any)
	return s
}
