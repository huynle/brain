// Package sdkcontract validates the supported public SDK contract.
package sdkcontract

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Validate checks the contract used by both SDK generators.
func Validate(data []byte) error {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc["openapi"] != "3.1.0" {
		return fmt.Errorf("OpenAPI 3.1.0 required")
	}
	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) == 0 || len(schemas) == 0 {
		return fmt.Errorf("paths and schemas required")
	}
	ids := map[string]bool{}
	for path, raw := range paths {
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("invalid path %q", path)
		}
		methods, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid path item")
		}
		for method, raw := range methods {
			if method == "parameters" {
				continue
			}
			if !strings.Contains(" get post put patch delete ", " "+method+" ") {
				return fmt.Errorf("unsupported method %q", method)
			}
			op, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid operation")
			}
			id, _ := op["operationId"].(string)
			if id == "" || ids[id] {
				return fmt.Errorf("missing/duplicate operationId %q", id)
			}
			ids[id] = true
			if op["x-brain-effect"] != "read" && op["x-brain-effect"] != "write" && op["x-brain-effect"] != "provider" {
				return fmt.Errorf("missing/invalid effect for %s", id)
			}
			if exposed, ok := op["x-brain-script"].(bool); !ok || exposed {
				return fmt.Errorf("script exposure must remain explicitly false: %s", id)
			}
		}
	}
	var refs func(any) error
	refs = func(v any) error {
		switch v := v.(type) {
		case map[string]any:
			for k, value := range v {
				if k == "$ref" {
					ref, ok := value.(string)
					if !ok || !strings.HasPrefix(ref, "#/components/schemas/") {
						return fmt.Errorf("unsupported reference %v", value)
					}
					if _, ok := schemas[strings.TrimPrefix(ref, "#/components/schemas/")]; !ok {
						return fmt.Errorf("unresolved reference %s", ref)
					}
				}
				if err := refs(value); err != nil {
					return err
				}
			}
		case []any:
			for _, value := range v {
				if err := refs(value); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return refs(doc)
}
