package openapi

import (
	"sort"

	"github.com/getkin/kin-openapi/openapi3"
)

type Operation struct {
	Method      string
	Path        string
	OperationID string
}

// LoadOperations parses an OpenAPI document and returns stable operation metadata.
func LoadOperations(spec []byte) ([]Operation, error) {
	doc, err := openapi3.NewLoader().LoadFromData(spec)
	if err != nil {
		return nil, err
	}
	operations := make([]Operation, 0)
	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			operations = append(operations, Operation{Method: method, Path: path, OperationID: operation.OperationID})
		}
	}
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].Path == operations[j].Path {
			return operations[i].Method < operations[j].Method
		}
		return operations[i].Path < operations[j].Path
	})
	return operations, nil
}
