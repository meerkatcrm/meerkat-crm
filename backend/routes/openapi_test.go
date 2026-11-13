package routes

import (
	"os"
	"testing"
)

// The committed spec is the contract the frontend's types are generated from, so
// it must match the operations.
func TestOpenAPISpecIsCurrent(t *testing.T) {
	want, err := OpenAPISpec()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../docs/openapi.json")
	if err != nil {
		t.Fatalf("read docs/openapi.json: %v (run make openapi)", err)
	}
	if string(got) != string(want) {
		t.Fatal("docs/openapi.json is out of date: run make openapi, then commit it and frontend/src/types/generated/api.ts")
	}
}
