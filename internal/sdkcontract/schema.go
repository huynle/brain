package sdkcontract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Schemas derives JSON wire shapes for the supplied named Go structs.
func Schemas(roots ...reflect.Type) (map[string]any, error) {
	out := map[string]any{}
	var shape func(reflect.Type) (map[string]any, error)
	nullable := func(s map[string]any) map[string]any {
		return map[string]any{"anyOf": []any{s, map[string]any{"type": "null"}}}
	}
	shape = func(root reflect.Type) (map[string]any, error) {
		if root == reflect.TypeOf(time.Time{}) {
			return map[string]any{"type": "string", "format": "date-time"}, nil
		}
		marshaler := reflect.TypeOf((*json.Marshaler)(nil)).Elem()
		if root.Implements(marshaler) || reflect.PointerTo(root).Implements(marshaler) {
			return nil, fmt.Errorf("custom JSON marshaler requires an explicit schema: %s", root)
		}
		switch root.Kind() {
		case reflect.Pointer:
			s, err := shape(root.Elem())
			if err != nil {
				return nil, err
			}
			return nullable(s), nil
		case reflect.Slice:
			if root.Elem().Kind() == reflect.Uint8 {
				return nullable(map[string]any{"type": "string", "contentEncoding": "base64"}), nil
			}
			s, err := shape(root.Elem())
			if err != nil {
				return nil, err
			}
			return nullable(map[string]any{"type": "array", "items": s}), nil
		case reflect.Map:
			if root.Key().Kind() != reflect.String {
				return nil, fmt.Errorf("unsupported map key %s", root.Key())
			}
			s, err := shape(root.Elem())
			if err != nil {
				return nil, err
			}
			return nullable(map[string]any{"type": "object", "additionalProperties": s}), nil
		case reflect.Interface:
			return map[string]any{}, nil
		case reflect.String:
			return map[string]any{"type": "string"}, nil
		case reflect.Bool:
			return map[string]any{"type": "boolean"}, nil
		case reflect.Int:
			return map[string]any{"type": "integer"}, nil
		case reflect.Int64:
			return map[string]any{"type": "integer", "format": "int64"}, nil
		case reflect.Float64:
			return map[string]any{"type": "number"}, nil
		case reflect.Struct:
		default:
			return nil, fmt.Errorf("unsupported type %s", root)
		}
		ref := map[string]any{"$ref": "#/components/schemas/" + root.Name()}
		if _, seen := out[root.Name()]; seen {
			return ref, nil
		}
		out[root.Name()] = nil // Break self-referential DTO cycles.
		properties := map[string]any{}
		var required []string
		for i := 0; i < root.NumField(); i++ {
			field := root.Field(i)
			if field.Anonymous {
				return nil, fmt.Errorf("embedded field requires an explicit schema: %s", field.Name)
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			if field.PkgPath != "" || tag[0] == "-" {
				continue
			}
			name := tag[0]
			if name == "" {
				name = field.Name
			}
			for _, option := range tag[1:] {
				if option != "omitempty" {
					return nil, fmt.Errorf("unsupported JSON tag option %q", option)
				}
			}
			if _, exists := properties[name]; exists {
				return nil, fmt.Errorf("duplicate JSON field %q", name)
			}
			fieldSchema, err := shape(field.Type)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", root.Name(), field.Name, err)
			}
			properties[name] = fieldSchema
			if !strings.Contains(","+field.Tag.Get("json")+",", ",omitempty,") {
				required = append(required, name)
			}
		}
		schema := map[string]any{"type": "object", "properties": properties}
		if len(required) > 0 {
			schema["required"] = required
		}
		out[root.Name()] = schema
		return ref, nil
	}
	for _, root := range roots {
		if root == nil || root.Kind() != reflect.Struct || root.Name() == "" {
			return nil, fmt.Errorf("named struct root required")
		}
		if _, err := shape(root); err != nil {
			return nil, err
		}
	}
	return out, nil
}
