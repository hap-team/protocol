package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/hap-team/protocol/internal/conformance"
	"gopkg.in/yaml.v3"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
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
