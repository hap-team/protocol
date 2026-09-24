package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/hap-team/protocol/definition"
)

// A source-owned job may contain richer fixture checks than the generic
// transport scenario (authentication, replay, cancellation, result schemas).
// The job executes those checks; it never signs an agent's claimed success.
type conformanceJob struct {
	Schema           string   `json:"schema"`
	Definition       string   `json:"definition"`
	Interface        string   `json:"interface"`
	WorkingDirectory string   `json:"workingDirectory"`
	Command          []string `json:"command"`
}

func runJob(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hap-conformance job", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jobPath := flags.String("job", "", "source-owned fixture verification job")
	evidence := evidenceFlags(flags)
	if flags.Parse(args) != nil {
		return 2
	}
	raw, err := os.ReadFile(*jobPath)
	if err != nil || len(raw) > 64<<10 {
		fmt.Fprintln(stderr, "cannot read conformance job")
		return 2
	}
	var job conformanceJob
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&job) != nil || job.Schema != "dev.hap.conformance-job/v1" || len(job.Command) == 0 || job.Definition == "" || job.WorkingDirectory == "" {
		fmt.Fprintln(stderr, "invalid conformance job")
		return 2
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fmt.Fprintln(stderr, "invalid trailing conformance job")
		return 2
	}
	directory, err := filepath.Abs(filepath.Dir(*jobPath))
	if err != nil {
		return 2
	}
	definitionPath := filepath.Join(directory, job.Definition)
	descriptor, err := os.ReadFile(definitionPath)
	if err != nil {
		fmt.Fprintln(stderr, "cannot read job definition")
		return 2
	}
	public, err := definition.Parse(descriptor)
	if err != nil {
		fmt.Fprintln(stderr, "invalid job definition")
		return 1
	}
	found := false
	for _, iface := range public.Interfaces {
		if iface.ID == job.Interface {
			found = true
		}
	}
	if !found {
		fmt.Fprintln(stderr, "job interface is not declared")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, job.Command[0], job.Command[1:]...)
	command.Dir = filepath.Join(directory, job.WorkingDirectory)
	// Fixture subprocesses receive no signer, service credentials, model keys or
	// database URL. The signer remains confined to this verification process.
	for _, name := range []string{"PATH", "HOME", "TMPDIR"} {
		if value, ok := os.LookupEnv(name); ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	command.Env = append(command.Env, "PGHOST=/dev/null", "PGPORT=1", "PGDATABASE=__no_database__", "PGUSER=__no_database__", "PGCONNECT_TIMEOUT=1", "HAP_AGENT_PROFILE=fixture")
	command.Stdout = stdout
	command.Stderr = stderr
	status, code := "succeeded", 0
	if command.Run() != nil {
		status, code = "failed", 1
	}
	after, err := os.ReadFile(definitionPath)
	if err != nil || !bytes.Equal(descriptor, after) {
		fmt.Fprintln(stderr, "definition changed during verification")
		return 2
	}
	if err := evidence.write(descriptor, raw, job.Interface, status); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return code
}
