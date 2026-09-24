package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hap-team/protocol/compatibility"
	"github.com/hap-team/protocol/definition"
	"github.com/hap-team/protocol/internal/conformance"
	"github.com/hap-team/receipts/go/receipts"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

type packagedScenario struct {
	Schema       string            `json:"schema"`
	InterfaceID  string            `json:"interfaceId"`
	Endpoint     string            `json:"endpoint"`
	Task         map[string]any    `json:"task"`
	Headers      map[string]string `json:"headers,omitempty"`
	Query        map[string]string `json:"query,omitempty"`
	ResultTarget string            `json:"resultTarget"`
	// Explicit fixture input identities to refresh with each run. Dotted nested
	// paths and executable hooks are deliberately unsupported.
	InputIDs map[string]string `json:"inputIds,omitempty"`
}
type packagedHTTP struct {
	scenario packagedScenario
	client   *http.Client
}

func (h packagedHTTP) call(ctx context.Context, method, route string, body any) (int, json.RawMessage, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
	}
	endpoint, err := url.Parse(h.scenario.Endpoint + route)
	if err != nil {
		return 0, nil, errors.New("endpoint invalid")
	}
	q := endpoint.Query()
	for k, v := range h.scenario.Query {
		q.Set(k, v)
	}
	endpoint.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return 0, nil, errors.New("request invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.scenario.Headers {
		req.Header.Set(k, v)
	}
	res, err := h.client.Do(req)
	if err != nil {
		return 0, nil, errors.New("packaged interface unavailable")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return 0, nil, errors.New("packaged response unavailable")
	}
	return res.StatusCode, data, nil
}
func validMessage(raw []byte) bool {
	v, e := conformance.ValidateMessage(raw)
	return e == nil && len(v) == 0
}
func packagedTask(s packagedScenario, scenario string, delay int, deadline time.Duration) (map[string]any, error) {
	raw, _ := json.Marshal(s.Task)
	var task map[string]any
	if json.Unmarshal(raw, &task) != nil {
		return nil, errors.New("task invalid")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	id := "conformance-" + scenario + "-" + hex.EncodeToString(nonce)
	task["task_id"] = id + "-task"
	task["run_id"] = id
	task["deadline"] = time.Now().Add(deadline).UTC().Format(time.RFC3339Nano)
	input, ok := task["input"].(map[string]any)
	if len(s.InputIDs) > 0 && !ok {
		return nil, errors.New("fixture identities invalid")
	}
	for field, kind := range s.InputIDs {
		switch kind {
		case "task":
			input[field] = task["task_id"]
		case "run":
			input[field] = id
		case "uuid":
			nonce[6] = (nonce[6] & 15) | 64
			nonce[8] = (nonce[8] & 63) | 128
			v := hex.EncodeToString(nonce)
			input[field] = v[:8] + "-" + v[8:12] + "-" + v[12:16] + "-" + v[16:20] + "-" + v[20:]
		default:
			return nil, errors.New("fixture identity kind invalid")
		}
	}
	if delay > 0 {
		ext, _ := task["extensions"].(map[string]any)
		if ext == nil {
			ext = map[string]any{}
		}
		ext["hap.team/fixture-delay-ms"] = delay
		task["extensions"] = ext
	}
	raw, _ = json.Marshal(task)
	if !validMessage(raw) {
		return nil, errors.New("fixture task does not conform")
	}
	return task, nil
}
func (h packagedHTTP) waitRun(ctx context.Context, run string, terminal bool) (json.RawMessage, error) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, raw, err := h.call(ctx, "GET", "/runs/"+url.PathEscape(run), nil)
		if err == nil && (status == 200 || status == 202) {
			var value map[string]any
			if json.Unmarshal(raw, &value) != nil {
				return nil, errors.New("run response invalid")
			}
			if !terminal || value["type"] == "result" {
				return raw, nil
			}
		}
		if status != 0 && status != 200 && status != 202 && status != 404 {
			return nil, errors.New("run lookup rejected")
		}
		select {
		case <-ctx.Done():
			return nil, errors.New("run did not reach expected state")
		case <-ticker.C:
		}
	}
}
func (h packagedHTTP) runTask(ctx context.Context, task map[string]any, cancel bool) (json.RawMessage, error) {
	done := make(chan error, 1)
	go func() {
		status, _, err := h.call(ctx, "POST", "/tasks", task)
		if err == nil && status != 200 && status != 202 {
			err = errors.New("task rejected")
		}
		done <- err
	}()
	run := task["run_id"].(string)
	if cancel {
		if _, err := h.waitRun(ctx, run, false); err != nil {
			return nil, err
		}
		request := map[string]any{"type": "cancel", "run_id": run}
		status, raw, err := h.call(ctx, "POST", "/runs/"+url.PathEscape(run)+"/cancel", request)
		if err != nil || status != 200 {
			return nil, errors.New("cancellation rejected")
		}
		requested, _ := json.Marshal(request)
		v, e := conformance.ValidateCancellationAcknowledgement(requested, raw)
		var ack struct {
			Accepted bool
			Terminal bool
		}
		if e != nil || len(v) != 0 || json.Unmarshal(raw, &ack) != nil || !ack.Accepted || ack.Terminal {
			return nil, errors.New("cancellation acknowledgement invalid")
		}
	}
	result, err := h.waitRun(ctx, run, true)
	if err != nil {
		return nil, err
	}
	select {
	case err = <-done:
		if err != nil {
			return nil, err
		}
	case <-ctx.Done():
		return nil, errors.New("task submission incomplete")
	}
	result, err = h.waitRun(ctx, run, true)
	if err != nil {
		return nil, err
	}
	if !validMessage(result) {
		return nil, errors.New("result envelope does not conform")
	}
	var value map[string]any
	_ = json.Unmarshal(result, &value)
	if value["run_id"] != task["run_id"] || value["task_id"] != task["task_id"] {
		return nil, errors.New("result identity mismatch")
	}
	return result, nil
}
func (h packagedHTTP) events(ctx context.Context, run string) ([]json.RawMessage, error) {
	status, raw, err := h.call(ctx, "GET", "/runs/"+url.PathEscape(run)+"/events", nil)
	if err != nil || status != 200 {
		return nil, errors.New("events unavailable")
	}
	var events []json.RawMessage
	if json.Unmarshal(raw, &events) != nil {
		var wrapper struct {
			Events []json.RawMessage `json:"events"`
		}
		if json.Unmarshal(raw, &wrapper) != nil {
			return nil, errors.New("events invalid")
		}
		events = wrapper.Events
	}
	if len(events) == 0 || len(events) > 4096 {
		return nil, errors.New("events missing or oversized")
	}
	for i, event := range events {
		var value struct {
			Type     string
			RunID    string `json:"run_id"`
			Sequence int
		}
		if !validMessage(event) || json.Unmarshal(event, &value) != nil || value.Type != "event" || value.RunID != run || value.Sequence != i+1 {
			return nil, errors.New("event order or identity invalid")
		}
	}
	return events, nil
}
func packagedSchema(root, relative string) (*jsonschema.Schema, error) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, errors.New("schema root unavailable")
	}
	root = canonicalRoot
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "\\") {
		return nil, errors.New("result schema invalid")
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, errors.New("result schema invalid")
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	compiler.LoadURL = func(address string) (io.ReadCloser, error) {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "https" || u.Host != "shipped.invalid" || u.RawQuery != "" {
			return nil, errors.New("external schema forbidden")
		}
		name := strings.TrimPrefix(u.Path, "/")
		clean := filepath.Clean(name)
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return nil, errors.New("schema path invalid")
		}
		p := filepath.Join(root, clean)
		real, err := filepath.EvalSymlinks(p)
		if err != nil || real != p {
			return nil, errors.New("schema path invalid")
		}
		raw, err := readPackagedFile(root, clean, 1<<20)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	return compiler.Compile("https://shipped.invalid/" + filepath.ToSlash(clean))
}
func verifyPackaged(ctx context.Context, descriptor []byte, schemaRoot string, scenarioRaw []byte) (packagedResult, error) {
	result := packagedResult{Schema: "oci-verifier-result/v1", Status: "failed", Suite: compatibility.PackagedSuiteID, ScenarioDigest: receipts.Digest(scenarioRaw), Checks: map[string]string{}}
	public, err := definition.Parse(descriptor)
	if err != nil {
		return result, err
	}
	var scenario packagedScenario
	decoder := json.NewDecoder(bytes.NewReader(scenarioRaw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&scenario) != nil || scenario.Schema != "hap-packaged-scenario/v1" || len(scenario.Task) == 0 {
		return result, errors.New("packaged scenario invalid")
	}
	endpoint, err := url.Parse(scenario.Endpoint)
	if err != nil || endpoint.Scheme != "http" || (endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "::1") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || strings.Trim(endpoint.Path, "/") != "" {
		return result, errors.New("verifier requires isolated loopback interface")
	}
	scenario.Endpoint = strings.TrimRight(scenario.Endpoint, "/")
	found := false
	for _, iface := range public.Interfaces {
		if iface.ID == scenario.InterfaceID && iface.Type == "http" {
			found = true
		}
	}
	if !found {
		return result, errors.New("HTTP interface not declared")
	}
	result.InterfaceID = scenario.InterfaceID
	result.Checks["descriptor"] = "passed"
	var contracts struct {
		Contracts struct {
			Result struct {
				Schema string `yaml:"schema"`
			} `yaml:"result"`
		} `yaml:"contracts"`
	}
	if yaml.Unmarshal(descriptor, &contracts) != nil {
		return result, errors.New("contracts invalid")
	}
	schema, err := packagedSchema(schemaRoot, contracts.Contracts.Result.Schema)
	if err != nil {
		return result, errors.New("shipped result schema unavailable")
	}
	if scenario.ResultTarget != "output" && scenario.ResultTarget != "envelope" {
		return result, errors.New("result target invalid")
	}
	h := packagedHTTP{scenario: scenario, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	// Startup polling is bounded; a responding but nonconforming handshake fails immediately.
	handshake := map[string]any{"type": "handshake", "supported_versions": []string{"0.2"}, "client": map[string]string{"name": "hap-conformance", "version": "1.0.0"}}
	var raw json.RawMessage
	var status int
	for {
		status, raw, err = h.call(ctx, "POST", "/handshake", handshake)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return result, errors.New("handshake unavailable")
		case <-time.After(50 * time.Millisecond):
		}
	}
	var ack struct {
		Type            string
		ProtocolVersion string `json:"protocol_version"`
		Agent           struct {
			Name    string
			Version string
		}
	}
	if status != 200 || !validMessage(raw) || json.Unmarshal(raw, &ack) != nil || ack.Type != "handshake_ack" || ack.ProtocolVersion != public.ProtocolVersion || ack.Agent.Name != public.Name || ack.Agent.Version != public.Version {
		return result, errors.New("handshake does not match shipped definition")
	}
	result.Checks["handshake"] = "passed"
	task, err := packagedTask(scenario, "success", 0, 30*time.Second)
	if err != nil {
		return result, err
	}
	terminal, err := h.runTask(ctx, task, false)
	if err != nil {
		return result, err
	}
	var value map[string]any
	_ = json.Unmarshal(terminal, &value)
	if value["status"] != "succeeded" {
		return result, errors.New("success fixture did not succeed")
	}
	var target any = value
	if scenario.ResultTarget == "output" {
		target = value["output"]
	}
	if schema.Validate(target) != nil {
		return result, errors.New("result violates shipped schema")
	}
	result.Checks["result_schema"] = "passed"
	events, err := h.events(ctx, task["run_id"].(string))
	if err != nil {
		return result, err
	}
	result.Checks["ordered_events"] = "passed"
	replay, err := h.runTask(ctx, task, false)
	if err != nil {
		return result, err
	}
	var other map[string]any
	_ = json.Unmarshal(replay, &other)
	before, _ := json.Marshal(value)
	after, _ := json.Marshal(other)
	replayEvents, e := h.events(ctx, task["run_id"].(string))
	a, _ := json.Marshal(events)
	b, _ := json.Marshal(replayEvents)
	if !bytes.Equal(before, after) || e != nil || !bytes.Equal(a, b) {
		return result, errors.New("duplicate task changed result or events")
	}
	result.Checks["duplicate_task"] = "passed"
	for _, which := range []string{"cancellation", "deadline"} {
		deadline := 30 * time.Second
		if which == "deadline" {
			deadline = 750 * time.Millisecond
		}
		task, err = packagedTask(scenario, which, 3000, deadline)
		if err != nil {
			return result, err
		}
		terminal, err = h.runTask(ctx, task, which == "cancellation")
		if err != nil {
			return result, err
		}
		_ = json.Unmarshal(terminal, &other)
		if which == "cancellation" && other["status"] != "cancelled" {
			return result, errors.New("cancelled task did not terminate cancelled")
		}
		if which == "deadline" {
			failure, _ := other["error"].(map[string]any)
			if other["status"] != "cancelled" && (other["status"] != "failed" || failure["code"] != "deadline_exceeded") {
				return result, errors.New("deadline did not terminate task")
			}
		}
		result.Checks[which] = "passed"
	}
	result.Status = "passed"
	return result, nil
}
func runPackaged(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hap-conformance packaged", flag.ContinueOnError)
	flags.SetOutput(stderr)
	descriptor := flags.String("descriptor", "/subject-definition", "shipped descriptor")
	schemas := flags.String("schemas", "/support", "shipped schema root")
	scenario := flags.String("scenario", "", "fixture scenario")
	output := flags.String("output", "/output/result.json", "verifier result")
	if flags.Parse(args) != nil || *scenario == "" {
		return 2
	}
	raw, err := readPackagedFile(filepath.Dir(*descriptor), filepath.Base(*descriptor), definition.MaxBytes)
	if err != nil {
		fmt.Fprintln(stderr, "shipped definition unavailable")
		return 1
	}
	fixture, err := readPackagedFile(filepath.Dir(*scenario), filepath.Base(*scenario), 1<<20)
	if err != nil {
		fmt.Fprintln(stderr, "scenario unavailable")
		return 1
	}
	root, err := filepath.Abs(*schemas)
	if err != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, checkErr := verifyPackaged(ctx, raw, root, fixture)
	encoded, _ := json.Marshal(result)
	if os.WriteFile(*output, encoded, 0600) != nil {
		fmt.Fprintln(stderr, "verification output unavailable")
		return 1
	}
	if checkErr != nil {
		fmt.Fprintln(stderr, checkErr)
		return 1
	}
	fmt.Fprintln(stdout, "packaged HTTP conformance passed")
	return 0
}
