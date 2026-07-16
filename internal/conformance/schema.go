package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

// Violation describes one descriptor conformance failure.
type Violation struct {
	Path    string
	Message string
}

func (v Violation) String() string {
	if v.Path == "" {
		return v.Message
	}
	return v.Path + ": " + v.Message
}

var (
	descriptorSchemaOnce sync.Once
	descriptorSchema     *jsonschema.Schema
	descriptorSchemaErr  error
)

// ValidateDescriptor validates a YAML or JSON HAP 0.2 agent descriptor.
func ValidateDescriptor(data []byte) ([]Violation, error) {
	var document any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode descriptor: %w", err)
	}

	jsonData, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode descriptor as JSON: %w", err)
	}
	if err := json.Unmarshal(jsonData, &document); err != nil {
		return nil, fmt.Errorf("decode descriptor JSON: %w", err)
	}

	schema, err := loadDescriptorSchema()
	if err != nil {
		return nil, err
	}

	var violations []Violation
	if err := schema.Validate(document); err != nil {
		validationErr, ok := err.(*jsonschema.ValidationError)
		if !ok {
			return nil, fmt.Errorf("validate descriptor: %w", err)
		}
		flattenValidationErrors(validationErr, &violations)
	}

	violations = append(violations, duplicateInterfaceIDViolations(document)...)
	return violations, nil
}

func loadDescriptorSchema() (*jsonschema.Schema, error) {
	descriptorSchemaOnce.Do(func() {
		_, sourceFile, _, ok := runtime.Caller(0)
		if !ok {
			descriptorSchemaErr = fmt.Errorf("locate conformance package")
			return
		}
		schemaDir := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "schemas", "0.2"))

		compiler := jsonschema.NewCompiler()
		compiler.Draft = jsonschema.Draft2020
		compiler.AssertFormat = true

		for _, name := range []string{"common.schema.json", "hap-agent.schema.json"} {
			data, err := os.ReadFile(filepath.Join(schemaDir, name))
			if err != nil {
				descriptorSchemaErr = fmt.Errorf("read %s: %w", name, err)
				return
			}
			url := "https://humanagentprotocol.com/schemas/0.2/" + name
			if err := compiler.AddResource(url, bytes.NewReader(data)); err != nil {
				descriptorSchemaErr = fmt.Errorf("load %s: %w", name, err)
				return
			}
		}

		descriptorSchema, descriptorSchemaErr = compiler.Compile(
			"https://humanagentprotocol.com/schemas/0.2/hap-agent.schema.json",
		)
	})
	if descriptorSchemaErr != nil {
		return nil, fmt.Errorf("compile descriptor schema: %w", descriptorSchemaErr)
	}
	return descriptorSchema, nil
}

func flattenValidationErrors(err *jsonschema.ValidationError, violations *[]Violation) {
	if len(err.Causes) == 0 {
		path := strings.TrimPrefix(strings.ReplaceAll(err.InstanceLocation, "/", "."), ".")
		*violations = append(*violations, Violation{
			Path:    path + " " + err.Message,
			Message: err.Message,
		})
		return
	}
	for _, cause := range err.Causes {
		flattenValidationErrors(cause, violations)
	}
}

func duplicateInterfaceIDViolations(document any) []Violation {
	root, ok := document.(map[string]any)
	if !ok {
		return nil
	}
	interfaces, ok := root["interfaces"].([]any)
	if !ok {
		return nil
	}

	seen := make(map[string]struct{}, len(interfaces))
	for index, raw := range interfaces {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, ok := entry["id"].(string)
		if !ok {
			continue
		}
		if _, exists := seen[id]; exists {
			return []Violation{{
				Path:    fmt.Sprintf("interfaces.%d.id", index),
				Message: fmt.Sprintf("duplicate interface id %q", id),
			}}
		}
		seen[id] = struct{}{}
	}
	return nil
}
