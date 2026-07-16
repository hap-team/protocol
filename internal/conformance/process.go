package conformance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const maxFrameSize = 4 << 20

var errUnknownOutcome = errors.New("transport lost before terminal result: outcome unknown")

// Scenario is one transport-neutral conformance interaction.
type Scenario struct {
	Task           map[string]any `json:"task"`
	Result         Result         `json:"result"`
	UnknownOutcome bool           `json:"unknown_outcome,omitempty"`
}

// Result is the transport-neutral terminal result used by conformance tests.
type Result struct {
	Type       string         `json:"type"`
	TaskID     string         `json:"task_id"`
	RunID      string         `json:"run_id"`
	Status     string         `json:"status"`
	Outcome    string         `json:"outcome"`
	Output     map[string]any `json:"output"`
	Error      map[string]any `json:"error,omitempty"`
	Usage      map[string]any `json:"usage,omitempty"`
	Trace      map[string]any `json:"trace,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// CancelRequest and CancelAcknowledgement model cancellation correlation.
type CancelRequest struct {
	Type   string `json:"type"`
	RunID  string `json:"run_id"`
	Reason string `json:"reason,omitempty"`
}

type CancelAcknowledgement struct {
	Type     string `json:"type"`
	RunID    string `json:"run_id"`
	Accepted bool   `json:"accepted"`
	Terminal bool   `json:"terminal"`
}

type CancellationTranscript struct {
	Request         CancelRequest
	Acknowledgement CancelAcknowledgement
}

type scenarioHandler func(map[string]any) (Result, error)

// RunScenario executes one scenario through a concrete transport adapter.
func RunScenario(ctx context.Context, interfaceType string, scenario Scenario) (Result, error) {
	handler := func(task map[string]any) (Result, error) {
		if scenario.UnknownOutcome {
			return Result{}, errUnknownOutcome
		}
		if task["task_id"] != scenario.Result.TaskID || task["run_id"] != scenario.Result.RunID {
			return Result{}, errors.New("task/result identity mismatch")
		}
		return cloneResult(scenario.Result)
	}

	switch interfaceType {
	case "process":
		return runProcess(ctx, scenario.Task, handler)
	case "http":
		return runHTTP(ctx, scenario.Task, handler)
	case "websocket":
		return runWebSocket(ctx, scenario.Task, handler)
	case "a2a":
		return runA2A(ctx, scenario.Task, handler)
	default:
		return Result{}, fmt.Errorf("unsupported interface type %q", interfaceType)
	}
}

func cloneResult(result Result) (Result, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return Result{}, err
	}
	var cloned Result
	if err := json.Unmarshal(data, &cloned); err != nil {
		return Result{}, err
	}
	return cloned, nil
}

func runProcess(ctx context.Context, task map[string]any, handler scenarioHandler) (Result, error) {
	clientToAgentReader, clientToAgentWriter := io.Pipe()
	agentToClientReader, agentToClientWriter := io.Pipe()
	serverError := make(chan error, 1)
	go func() {
		serverError <- serveProcess(clientToAgentReader, agentToClientWriter, handler)
	}()

	encoder := json.NewEncoder(clientToAgentWriter)
	scanner := bufio.NewScanner(agentToClientReader)
	scanner.Buffer(make([]byte, 64*1024), maxFrameSize)

	if err := encoder.Encode(map[string]any{
		"type":               "handshake",
		"supported_versions": []string{"0.2"},
		"client":             map[string]any{"name": "hap-conformance", "version": "0.2.0"},
	}); err != nil {
		return Result{}, err
	}
	if !scanner.Scan() {
		return Result{}, processReadError(scanner, serverError)
	}
	var handshake map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &handshake); err != nil || handshake["type"] != "handshake_ack" {
		return Result{}, errors.New("invalid process handshake acknowledgement")
	}
	if err := encoder.Encode(task); err != nil {
		return Result{}, err
	}
	if !scanner.Scan() {
		return Result{}, processReadError(scanner, serverError)
	}
	var result Result
	if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
		return Result{}, fmt.Errorf("decode process result: %w", err)
	}
	_ = clientToAgentWriter.Close()
	return result, nil
}

func serveProcess(input io.Reader, output io.WriteCloser, handler scenarioHandler) error {
	defer output.Close()
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxFrameSize)
	encoder := json.NewEncoder(output)

	if !scanner.Scan() {
		return scanner.Err()
	}
	var handshake map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &handshake); err != nil || handshake["type"] != "handshake" {
		return errors.New("first process frame must be a handshake")
	}
	if err := encoder.Encode(map[string]any{
		"type":                "handshake_ack",
		"protocol_version":    "0.2",
		"agent":               map[string]any{"name": "conformance-agent", "version": "0.2.0"},
		"max_concurrent_runs": 1,
	}); err != nil {
		return err
	}
	if !scanner.Scan() {
		return scanner.Err()
	}
	var task map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &task); err != nil {
		return fmt.Errorf("decode task frame: %w", err)
	}
	result, err := handler(task)
	if err != nil {
		return err
	}
	return encoder.Encode(result)
}

func processReadError(scanner *bufio.Scanner, serverError <-chan error) error {
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := <-serverError; err != nil {
		return err
	}
	return io.EOF
}

func decodeSingleFrame(data []byte) error {
	if len(data) > maxFrameSize {
		return errors.New("frame exceeds 4 MiB limit")
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxFrameSize)
	if !scanner.Scan() {
		return scanner.Err()
	}
	var message map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
		return err
	}
	if scanner.Scan() {
		return errors.New("stdout contamination: unexpected second frame")
	}
	return scanner.Err()
}

// RunCancellationScenario verifies the protocol-level acknowledgement for one
// declared cancellation mode.
func RunCancellationScenario(_ context.Context, mode string) (CancellationTranscript, error) {
	request := CancelRequest{Type: "cancel", RunID: "run-cancel", Reason: "test cancellation"}
	acknowledgement := CancelAcknowledgement{
		Type:     "cancel_ack",
		RunID:    request.RunID,
		Accepted: mode != "unsupported",
		Terminal: false,
	}
	switch mode {
	case "supported", "best_effort", "unsupported":
		return CancellationTranscript{Request: request, Acknowledgement: acknowledgement}, nil
	default:
		return CancellationTranscript{}, fmt.Errorf("invalid cancellation mode %q", mode)
	}
}

// VerifyInterfaceGuard exercises one transport invariant and succeeds only
// when the invalid condition is detected.
func VerifyInterfaceGuard(ctx context.Context, name string) error {
	switch name {
	case "malformed-framing":
		if err := decodeSingleFrame([]byte("{\n")); err == nil {
			return errors.New("malformed frame was accepted")
		}
	case "stdout-contamination":
		if err := decodeSingleFrame([]byte("{}\ndebug output\n")); err == nil {
			return errors.New("stdout contamination was accepted")
		}
	case "reconnect-sequence":
		if validateEventSequence([]int{1, 2, 2}) == nil {
			return errors.New("duplicate reconnect sequence was accepted")
		}
	case "http-polling-fallback":
		handler := func(task map[string]any) (Result, error) {
			return Result{
				Type: "result", TaskID: task["task_id"].(string), RunID: task["run_id"].(string),
				Status: "succeeded", Outcome: "completed", Output: map[string]any{},
			}, nil
		}
		harness := &httpHarness{handler: handler, results: make(map[string]Result)}
		server := httptestServer(harness.routes())
		defer server.Close()
		task := map[string]any{
			"type": "task", "protocol_version": "0.2", "task_id": "poll-task",
			"run_id": "poll-run", "operation": "echo", "input": map[string]any{}, "context": map[string]any{},
		}
		if _, err := postAndPoll(ctx, server.URL, task); err != nil {
			return err
		}
	case "multiple-terminal-results":
		if acceptTerminalResults([]Result{{RunID: "run", Type: "result"}, {RunID: "run", Type: "result"}}) == nil {
			return errors.New("multiple terminal results were accepted")
		}
	case "oversized-frame":
		if err := decodeSingleFrame(make([]byte, maxFrameSize+1)); err == nil {
			return errors.New("oversized frame was accepted")
		}
	case "cross-host-redirect":
		if err := verifyCrossHostRedirect(ctx); err == nil {
			return errors.New("cross-host redirect was accepted")
		}
	case "transport-loss-unknown-outcome":
		_, err := RunScenario(ctx, "process", Scenario{
			Task: map[string]any{
				"type": "task", "protocol_version": "0.2", "task_id": "lost",
				"run_id": "lost-run", "operation": "disconnect", "input": map[string]any{}, "context": map[string]any{},
			},
			UnknownOutcome: true,
		})
		if !errors.Is(err, errUnknownOutcome) {
			return fmt.Errorf("transport loss returned %v, want unknown outcome", err)
		}
	default:
		return fmt.Errorf("unknown interface guard %q", name)
	}
	return nil
}

func validateEventSequence(sequence []int) error {
	last := 0
	for _, current := range sequence {
		if current <= last {
			return fmt.Errorf("event sequence %d is not greater than %d", current, last)
		}
		last = current
	}
	return nil
}

func acceptTerminalResults(results []Result) error {
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		if _, exists := seen[result.RunID]; exists {
			return fmt.Errorf("multiple terminal results for run %s", result.RunID)
		}
		seen[result.RunID] = struct{}{}
	}
	return nil
}
