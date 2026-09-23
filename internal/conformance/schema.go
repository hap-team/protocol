package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/hap-team/protocol/schemas"
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
	messageSchemaOnce    sync.Once
	messageSchema        *jsonschema.Schema
	messageSchemaErr     error
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

// ValidateMessage validates one canonical HAP 0.2 JSON message.
func ValidateMessage(data []byte) ([]Violation, error) {
	var document any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}

	schema, err := loadMessageSchema()
	if err != nil {
		return nil, err
	}
	if err := schema.Validate(document); err != nil {
		validationErr, ok := err.(*jsonschema.ValidationError)
		if !ok {
			return nil, fmt.Errorf("validate message: %w", err)
		}
		var violations []Violation
		flattenValidationErrors(validationErr, &violations)
		return violations, nil
	}
	return nil, nil
}

// ValidateCancellationAcknowledgement verifies an acknowledgement belongs to
// the cancellation request it answers.
func ValidateCancellationAcknowledgement(request, acknowledgement []byte) ([]Violation, error) {
	if violations, err := ValidateMessage(request); err != nil || len(violations) != 0 {
		return violations, err
	}
	if violations, err := ValidateMessage(acknowledgement); err != nil || len(violations) != 0 {
		return violations, err
	}

	var cancel struct {
		Type  string `json:"type"`
		RunID string `json:"run_id"`
	}
	var ack struct {
		Type  string `json:"type"`
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(request, &cancel); err != nil {
		return nil, fmt.Errorf("decode cancellation request: %w", err)
	}
	if err := json.Unmarshal(acknowledgement, &ack); err != nil {
		return nil, fmt.Errorf("decode cancellation acknowledgement: %w", err)
	}
	if cancel.Type != "cancel" || ack.Type != "cancel_ack" {
		return []Violation{{
			Path:    "type",
			Message: "expected cancel followed by cancel_ack",
		}}, nil
	}
	if cancel.RunID != ack.RunID {
		return []Violation{{
			Path:    "run_id",
			Message: "cancellation acknowledgement must use the request run_id",
		}}, nil
	}
	return nil, nil
}

func loadDescriptorSchema() (*jsonschema.Schema, error) {
	descriptorSchemaOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		compiler.Draft = jsonschema.Draft2020
		compiler.AssertFormat = true

		for _, name := range []string{"common.schema.json", "hap-agent.schema.json"} {
			data, err := schemas.Files.ReadFile("0.2/" + name)
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

func loadMessageSchema() (*jsonschema.Schema, error) {
	messageSchemaOnce.Do(func() {
		compiler, err := newSchemaCompiler()
		if err != nil {
			messageSchemaErr = err
			return
		}
		messageSchema, messageSchemaErr = compiler.Compile(
			"https://humanagentprotocol.com/schemas/0.2/message.schema.json",
		)
	})
	if messageSchemaErr != nil {
		return nil, fmt.Errorf("compile message schema: %w", messageSchemaErr)
	}
	return messageSchema, nil
}

func newSchemaCompiler() (*jsonschema.Compiler, error) {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	compiler.AssertFormat = true
	for _, name := range []string{
		"common.schema.json",
		"hap-agent.schema.json",
		"task.schema.json",
		"event.schema.json",
		"result.schema.json",
		"cancel.schema.json",
		"handshake.schema.json",
		"message.schema.json",
	} {
		data, err := schemas.Files.ReadFile("0.2/" + name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		url := "https://humanagentprotocol.com/schemas/0.2/" + name
		if err := compiler.AddResource(url, bytes.NewReader(data)); err != nil {
			return nil, fmt.Errorf("load %s: %w", name, err)
		}
	}
	return compiler, nil
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
