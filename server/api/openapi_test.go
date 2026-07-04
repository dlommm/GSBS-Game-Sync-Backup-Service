package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// The OpenAPI spec must describe exactly the routes the server implements —
// no undocumented endpoints, no documented-but-nonexistent ones. This test
// fails the build on drift so docs/openapi.json stays honest.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	var spec struct {
		OpenAPI string                            `json:"openapi"`
		Paths   map[string]map[string]interface{} `json:"paths"`
	}
	if err := json.Unmarshal(openapiSpec, &spec); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.") {
		t.Fatalf("openapi field = %q, want 3.x", spec.OpenAPI)
	}

	// Build the set of (method, path) pairs the spec documents.
	specSet := map[string]bool{}
	for path, methods := range spec.Paths {
		for method := range methods {
			specSet[strings.ToUpper(method)+" "+path] = true
		}
	}

	// Build the set the code implements. GET/POST/DELETE on the same path are
	// distinct operations; the spec lists them under the same path object.
	codeSet := map[string]bool{}
	for _, rt := range Routes() {
		codeSet[rt.Method+" "+rt.Path] = true
	}

	for key := range codeSet {
		if !specSet[key] {
			t.Errorf("route %q is implemented but missing from openapi.json", key)
		}
	}
	for key := range specSet {
		if !codeSet[key] {
			t.Errorf("openapi.json documents %q which is not an implemented route", key)
		}
	}
}
