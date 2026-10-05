// Package brain is the public Brain HTTP SDK. It exports no server authority.
package brain

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -generate types -package brain -o schema.gen.go ../../api/openapi.yaml
