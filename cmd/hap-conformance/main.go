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

	defaultscenarios "github.com/hap-team/protocol/conformance/scenarios"
	"github.com/hap-team/protocol/internal/conformance"
	"gopkg.in/yaml.v3"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "job" {
		return runJob(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "interface" {
		return runInterface(args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("hap-conformance", flag.ContinueOnError)
	flags.SetOutput(stderr)
	evidence := evidenceFlags(flags)
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
	if err := evidence.write(data, nil, "", "succeeded"); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	fmt.Fprintf(stdout, "HAP %s %s@%s\n", descriptor.HAP, descriptor.Agent.Name, descriptor.Agent.Version)
	return 0
}

func runInterface(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hap-conformance interface", flag.ContinueOnError)
	flags.SetOutput(stderr)
	evidence := evidenceFlags(flags)
	descriptorPath := flags.String("descriptor", "", "path to a HAP agent descriptor")
	interfaceID := flags.String("interface", "", "descriptor-local interface ID")
	endpointOverride := flags.String("endpoint", "", "runtime endpoint override for HTTP, WebSocket, or A2A")
	scenarioPath := flags.String("scenario", "", "optional conformance scenario JSON")
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
			ID       string   `yaml:"id"`
			Type     string   `yaml:"type"`
			Command  []string `yaml:"command"`
			Endpoint string   `yaml:"endpoint"`
		} `yaml:"interfaces"`
	}
	if err := yaml.Unmarshal(data, &descriptor); err != nil {
		fmt.Fprintf(stderr, "descriptor interfaces: %v\n", err)
		return 2
	}
	var declared struct {
		Type     string
		Command  []string
		Endpoint string
	}
	for _, candidate := range descriptor.Interfaces {
		if candidate.ID == *interfaceID {
			declared.Type = candidate.Type
			declared.Command = candidate.Command
			declared.Endpoint = candidate.Endpoint
			break
		}
	}
	if declared.Type == "" {
		fmt.Fprintf(stderr, "interface %q not declared\n", *interfaceID)
		return 1
	}

	scenarioData := defaultscenarios.DefaultSuccess
	if *scenarioPath != "" {
		scenarioData, err = os.ReadFile(*scenarioPath)
		if err != nil {
			fmt.Fprintf(stderr, "scenario: %v\n", err)
			return 2
		}
	}
	var scenario conformance.Scenario
	if err := json.Unmarshal(scenarioData, &scenario); err != nil {
		fmt.Fprintf(stderr, "scenario: %v\n", err)
		return 2
	}
	endpoint := declared.Endpoint
	if *endpointOverride != "" {
		endpoint = *endpointOverride
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var result conformance.Result
	switch declared.Type {
	case "process":
		result, err = conformance.RunProcessAgent(ctx, declared.Command, filepath.Dir(*descriptorPath), scenario)
	case "http":
		result, err = conformance.RunHTTPAgent(ctx, endpoint, scenario)
	case "websocket":
		result, err = conformance.RunWebSocketAgent(ctx, endpoint, scenario)
	case "a2a":
		result, err = conformance.RunA2AAgent(ctx, endpoint, scenario)
	default:
		err = fmt.Errorf("unsupported interface type %q", declared.Type)
	}
	if err != nil {
		if issueErr := evidence.write(data, scenarioData, *interfaceID, "failed"); issueErr != nil {
			fmt.Fprintln(stderr, issueErr)
			return 2
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	if result.Type != "result" ||
		result.TaskID != scenario.Result.TaskID ||
		result.RunID != scenario.Result.RunID ||
		result.Status != scenario.Result.Status ||
		result.Outcome != scenario.Result.Outcome {
		if err := evidence.write(data, scenarioData, *interfaceID, "failed"); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		fmt.Fprintln(stderr, "interface result differs from scenario")
		return 1
	}
	if err := evidence.write(data, scenarioData, *interfaceID, "succeeded"); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	fmt.Fprintf(stdout, "PASS %s (%s)\n", *interfaceID, declared.Type)
	return 0
}
