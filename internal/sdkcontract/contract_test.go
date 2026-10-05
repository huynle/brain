package sdkcontract

import (
	"strings"
	"testing"
)

const validContract = `openapi: 3.1.0
info: {title: Brain, version: 1.0.0}
paths:
  /health:
    get:
      operationId: health.get
      x-brain-effect: read
      x-brain-script: false
      x-brain-profile: single
      security: []
      responses:
        '200':
          description: Healthy
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Health'}
components:
  schemas:
    Health:
      type: object
      required: [status]
      properties:
        status: {type: string}
`

func TestValidateContract(t *testing.T) {
	if err := Validate([]byte(validContract)); err != nil {
		t.Fatalf("valid OpenAPI SDK contract rejected: %v", err)
	}
}

func TestValidateRejectsContractDrift(t *testing.T) {
	for name, input := range map[string]string{
		"version":             strings.Replace(validContract, "3.1.0", "3.0.0", 1),
		"missing operation":   strings.Replace(validContract, "operationId: health.get", "operationId: ''", 1),
		"unknown schema":      strings.Replace(validContract, "schemas/Health", "schemas/Unknown", 1),
		"missing effects":     strings.Replace(validContract, "x-brain-effect: read", "x-brain-effect: ''", 1),
		"accidental exposure": strings.Replace(validContract, "x-brain-script: false", "x-brain-script: true", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if Validate([]byte(input)) == nil {
				t.Fatal("invalid contract accepted")
			}
		})
	}
}
