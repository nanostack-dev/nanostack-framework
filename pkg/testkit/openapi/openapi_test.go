package openapi

import "testing"

func TestLoadOperations(t *testing.T) {
	spec := []byte(`openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths:
  /health:
    get:
      operationId: getHealth
      responses:
        '200':
          description: ok
`)
	operations, err := LoadOperations(spec)
	if err != nil {
		t.Fatalf("LoadOperations returned error: %v", err)
	}
	if len(operations) != 1 || operations[0].OperationID != "getHealth" {
		t.Fatalf("unexpected operations: %+v", operations)
	}
}
