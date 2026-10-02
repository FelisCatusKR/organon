// Package model holds the JSON types of the public API (/api/v1).
//
// The types in model.gen.go are generated from api/openapi.yaml, which is the
// contract: change the YAML, then run `go generate ./...`. Engine results are
// decoded into these types and encoded again, so a response can only contain
// what the contract describes.
package model

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../openapi.yaml

// Normalize makes empty collections encode as [] rather than null.
func (t *Task) Normalize() {
	if t.Tags == nil {
		t.Tags = []string{}
	}
}
