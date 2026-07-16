package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/hap-team/protocol/internal/conformance"
	"gopkg.in/yaml.v3"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "interface" {
		return runInterface(args[1:], stdout, stderr)
	}

	flags := flag.NewFlagSet("hap-conformance", flag.ContinueOnError)
	flags.SetOutput(stderr)
	schemaPath := flags.String("schema", "", "path to the HAP agent descriptor schema")
	descriptorPath := flags.String("file", "", "path to a HAP agent descriptor")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *schemaPath == "" || *descriptorPath == "" {
		fmt.Fprintln(stderr, "--schema and --file are required")
		return 2
	}
	if _, err := os.Stat(*schemaPath); err != nil {
		fmt.Fprintf(stderr, "schema: %v\n", err)
		return 2
	}

	data, err := os.ReadFile(*descriptorPath)
	if err != nil {
		fmt.Fprintf(stderr, "descriptor: %v\n", err)
		return 2
	}
	violations, err := conformance.ValidateDescriptor(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if len(violations) != 0 {
		for _, violation := range violations {
			fmt.Fprintln(stderr, violation)
		}
		return 1
	}

	var descriptor struct {
		HAP   string `yaml:"hap"`
		Agent struct {
			Name    string `yaml:"name"`
			Version string `yaml:"version"`
		} `yaml:"agent"`
	}
	if err := yaml.Unmarshal(data, &descriptor); err != nil {
		fmt.Fprintf(stderr, "descriptor identity: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "HAP %s %s@%s\n", descriptor.HAP, descriptor.Agent.Name, descriptor.Agent.Version)
	return 0
}

func runInterface(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hap-conformance interface", flag.ContinueOnError)
	flags.SetOutput(stderr)
	descriptorPath := flags.String("descriptor", "", "path to a HAP agent descriptor")
	interfaceID := flags.String("interface", "", "descriptor-local interface ID")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *descriptorPath == "" || *interfaceID == "" {
		fmt.Fprintln(stderr, "--descriptor and --interface are required")
		return 2
	}

	data, err := os.ReadFile(*descriptorPath)
	if err != nil {
		fmt.Fprintf(stderr, "descriptor: %v\n", err)
		return 2
	}
	violations, err := conformance.ValidateDescriptor(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if len(violations) != 0 {
		for _, violation := range violations {
			fmt.Fprintln(stderr, violation)
		}
		return 1
	}

	var descriptor struct {
		Interfaces []struct {
			ID   string `yaml:"id"`
			Type string `yaml:"type"`
		} `yaml:"interfaces"`
	}
	if err := yaml.Unmarshal(data, &descriptor); err != nil {
		fmt.Fprintf(stderr, "descriptor interfaces: %v\n", err)
		return 2
	}
	var interfaceType string
	for _, declared := range descriptor.Interfaces {
		if declared.ID == *interfaceID {
			interfaceType = declared.Type
			break
		}
	}
	if interfaceType == "" {
		fmt.Fprintf(stderr, "interface %q not declared\n", *interfaceID)
		return 1
	}

	scenarioPath := filepath.Join(filepath.Dir(*descriptorPath), "..", "scenarios", "success.json")
	scenarioData, err := os.ReadFile(scenarioPath)
	if err != nil {
		fmt.Fprintf(stderr, "scenario: %v\n", err)
		return 2
	}
	var scenario conformance.Scenario
	if err := json.Unmarshal(scenarioData, &scenario); err != nil {
		fmt.Fprintf(stderr, "scenario: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := conformance.RunScenario(ctx, interfaceType, scenario)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if result.Status != scenario.Result.Status || result.Outcome != scenario.Result.Outcome {
		fmt.Fprintln(stderr, "interface result differs from scenario")
		return 1
	}
	fmt.Fprintf(stdout, "PASS %s (%s)\n", *interfaceID, interfaceType)
	return 0
}
