// Package definition validates agent-owned HAP definitions without fetching or
// executing their interfaces. Public contains only browser-safe identifiers.
package definition

import (
	"errors"
	"github.com/hap-team/protocol/internal/conformance"
	"gopkg.in/yaml.v3"
)

type Interface struct {
	ID   string `json:"id" yaml:"id"`
	Type string `json:"type" yaml:"type"`
}

const MaxBytes = 256 << 10

type Public struct {
	ProtocolVersion string      `json:"protocolVersion"`
	Name            string      `json:"name"`
	Version         string      `json:"version"`
	Capabilities    []string    `json:"capabilities"`
	Interfaces      []Interface `json:"interfaces"`
}

func Parse(raw []byte) (Public, error) {
	if len(raw) == 0 || len(raw) > MaxBytes {
		return Public{}, errors.New("definition size is invalid")
	}
	violations, err := conformance.ValidateDescriptor(raw)
	if err != nil || len(violations) > 0 {
		return Public{}, errors.New("definition does not conform to HAP 0.2")
	}
	var doc struct {
		HAP   string `yaml:"hap"`
		Agent struct {
			Name    string `yaml:"name"`
			Version string `yaml:"version"`
		} `yaml:"agent"`
		Capabilities []struct {
			ID string `yaml:"id"`
		} `yaml:"capabilities"`
		Interfaces []Interface `yaml:"interfaces"`
	}
	if yaml.Unmarshal(raw, &doc) != nil {
		return Public{}, errors.New("definition is invalid")
	}
	result := Public{ProtocolVersion: doc.HAP, Name: doc.Agent.Name, Version: doc.Agent.Version, Capabilities: []string{}, Interfaces: doc.Interfaces}
	for _, capability := range doc.Capabilities {
		result.Capabilities = append(result.Capabilities, capability.ID)
	}
	return result, nil
}
