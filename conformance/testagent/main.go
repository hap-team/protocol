package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"nhooyr.io/websocket"
)

func main() {
	mode := "process"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	switch mode {
	case "process":
		if err := serveProcess(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "http":
		address := "127.0.0.1:8443"
		if value := os.Getenv("HAP_TESTAGENT_ADDR"); value != "" {
			address = value
		}
		if err := http.ListenAndServe(address, routes()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", mode)
		os.Exit(2)
	}
}

func serveProcess() error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	encoder := json.NewEncoder(os.Stdout)
	if !scanner.Scan() {
		return scanner.Err()
	}
	var handshake map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &handshake); err != nil || handshake["type"] != "handshake" {
		return fmt.Errorf("handshake required")
	}
	if err := encoder.Encode(handshakeAcknowledgement()); err != nil {
		return err
	}
	for scanner.Scan() {
		var task map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &task); err != nil {
			return err
		}
		if err := encoder.Encode(handleTask(task)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, request *http.Request) {
		var task map[string]any
		if json.NewDecoder(request.Body).Decode(&task) != nil {
			http.Error(w, "invalid task", http.StatusBadRequest)
			return
		}
		writeJSON(w, handleTask(task))
	})
	mux.HandleFunc("POST /a2a", func(w http.ResponseWriter, request *http.Request) {
		var envelope struct {
			JSONRPC string `json:"jsonrpc"`
			ID      string `json:"id"`
			Params  struct {
				Message map[string]any `json:"message"`
			} `json:"params"`
		}
		if json.NewDecoder(request.Body).Decode(&envelope) != nil || envelope.JSONRPC != "2.0" {
			http.Error(w, "invalid A2A message", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{
			"jsonrpc": "2.0", "id": envelope.ID, "result": handleTask(envelope.Params.Message),
		})
	})
	mux.HandleFunc("/ws", serveWebSocket)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	})
	return http.MaxBytesHandler(mux, 4<<20)
}

func serveWebSocket(w http.ResponseWriter, request *http.Request) {
	connection, err := websocket.Accept(w, request, nil)
	if err != nil {
		return
	}
	defer connection.Close(websocket.StatusNormalClosure, "")
	_, data, err := connection.Read(request.Context())
	if err != nil {
		return
	}
	var handshake map[string]any
	if json.Unmarshal(data, &handshake) != nil || handshake["type"] != "handshake" {
		connection.Close(websocket.StatusPolicyViolation, "handshake required")
		return
	}
	data, _ = json.Marshal(handshakeAcknowledgement())
	if connection.Write(request.Context(), websocket.MessageText, data) != nil {
		return
	}
	_, data, err = connection.Read(request.Context())
	if err != nil {
		return
	}
	var task map[string]any
	if json.Unmarshal(data, &task) != nil {
		return
	}
	data, _ = json.Marshal(handleTask(task))
	_ = connection.Write(request.Context(), websocket.MessageText, data)
}

func handleTask(task map[string]any) map[string]any {
	input, _ := task["input"].(map[string]any)
	message, _ := input["message"].(string)
	if message == "" {
		return map[string]any{
			"type": "result", "task_id": task["task_id"], "run_id": task["run_id"],
			"status": "failed", "outcome": "invalid_input",
			"error": map[string]any{"code": "invalid_input", "message": "message is required", "retryable": false},
		}
	}
	result := map[string]any{
		"type": "result", "task_id": task["task_id"], "run_id": task["run_id"],
		"status": "succeeded", "outcome": "completed",
		"output": map[string]any{"message": message},
		"usage": map[string]any{
			"measurements": []any{map[string]any{"name": "agent.requests", "quantity": 1, "unit": "request"}},
			"cost":         map[string]any{"amount": "0", "currency": "USD", "source": "agent"},
		},
	}
	if trace, ok := task["trace"]; ok {
		result["trace"] = trace
	}
	return result
}

func handshakeAcknowledgement() map[string]any {
	return map[string]any{
		"type": "handshake_ack", "protocol_version": "0.2",
		"agent":               map[string]any{"name": "conformance-agent", "version": "0.2.0"},
		"max_concurrent_runs": 4,
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
