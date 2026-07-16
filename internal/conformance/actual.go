package conformance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func RunProcessAgent(ctx context.Context, command []string, directory string, scenario Scenario) (Result, error) {
	if len(command) == 0 {
		return Result{}, fmt.Errorf("process command is empty")
	}
	executable := command[0]
	if !filepath.IsAbs(executable) && filepath.Base(executable) != executable {
		executable = filepath.Join(directory, executable)
	}
	process := exec.CommandContext(ctx, executable, command[1:]...)
	process.Dir = directory
	stdin, err := process.StdinPipe()
	if err != nil {
		return Result{}, err
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	var stderr bytes.Buffer
	process.Stderr = &stderr
	if err := process.Start(); err != nil {
		return Result{}, err
	}
	encoder := json.NewEncoder(stdin)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxFrameSize)
	if err := encoder.Encode(map[string]any{
		"type": "handshake", "supported_versions": []string{"0.2"},
		"client": map[string]string{"name": "hap-conformance", "version": "0.2.0"},
	}); err != nil {
		return Result{}, err
	}
	if !scanner.Scan() {
		return Result{}, fmt.Errorf("process handshake: %s", stderr.String())
	}
	var acknowledgement struct {
		Type            string `json:"type"`
		ProtocolVersion string `json:"protocol_version"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &acknowledgement); err != nil ||
		acknowledgement.Type != "handshake_ack" ||
		acknowledgement.ProtocolVersion != "0.2" {
		return Result{}, fmt.Errorf("invalid process handshake acknowledgement")
	}
	if err := encoder.Encode(scenario.Task); err != nil {
		return Result{}, err
	}
	for scanner.Scan() {
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
			return Result{}, err
		}
		if header.Type != "result" {
			continue
		}
		var result Result
		if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
			return Result{}, err
		}
		_ = stdin.Close()
		_ = process.Wait()
		return result, nil
	}
	return Result{}, fmt.Errorf("process returned no terminal result: %s", stderr.String())
}

func RunHTTPAgent(ctx context.Context, endpoint string, scenario Scenario) (Result, error) {
	requestBody, err := json.Marshal(scenario.Task)
	if err != nil {
		return Result{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/tasks", bytes.NewReader(requestBody))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return Result{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("HTTP agent returned %s", response.Status)
	}
	var result Result
	return result, json.NewDecoder(response.Body).Decode(&result)
}

func RunWebSocketAgent(ctx context.Context, endpoint string, scenario Scenario) (Result, error) {
	connection, _, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		return Result{}, err
	}
	defer connection.CloseNow()
	if err := wsjson.Write(ctx, connection, map[string]any{
		"type": "handshake", "supported_versions": []string{"0.2"},
		"client": map[string]string{"name": "hap-conformance", "version": "0.2.0"},
	}); err != nil {
		return Result{}, err
	}
	var acknowledgement struct {
		Type            string `json:"type"`
		ProtocolVersion string `json:"protocol_version"`
	}
	if err := wsjson.Read(ctx, connection, &acknowledgement); err != nil ||
		acknowledgement.Type != "handshake_ack" ||
		acknowledgement.ProtocolVersion != "0.2" {
		return Result{}, fmt.Errorf("invalid WebSocket handshake acknowledgement")
	}
	if err := wsjson.Write(ctx, connection, scenario.Task); err != nil {
		return Result{}, err
	}
	for {
		var message json.RawMessage
		if err := wsjson.Read(ctx, connection, &message); err != nil {
			return Result{}, err
		}
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(message, &header); err != nil {
			return Result{}, err
		}
		if header.Type == "result" {
			var result Result
			return result, json.Unmarshal(message, &result)
		}
	}
}

func RunA2AAgent(ctx context.Context, endpoint string, scenario Scenario) (Result, error) {
	runID, _ := scenario.Task["run_id"].(string)
	envelope := map[string]any{
		"jsonrpc": "2.0", "id": runID, "method": "message/send",
		"params": map[string]any{"message": scenario.Task},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return Result{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return Result{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("A2A agent returned %s", response.Status)
	}
	var reply struct {
		JSONRPC string `json:"jsonrpc"`
		ID      string `json:"id"`
		Result  Result `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
		return Result{}, err
	}
	if reply.JSONRPC != "2.0" || reply.ID != runID {
		return Result{}, fmt.Errorf("invalid A2A response correlation")
	}
	return reply.Result, nil
}
