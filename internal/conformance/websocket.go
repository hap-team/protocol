package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	"nhooyr.io/websocket"
)

func runWebSocket(ctx context.Context, task map[string]any, handler scenarioHandler) (Result, error) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		connection, err := websocket.Accept(w, request, &websocket.AcceptOptions{
			OriginPatterns: []string{"127.0.0.1"},
		})
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
		ack, _ := json.Marshal(map[string]any{
			"type":                "handshake_ack",
			"protocol_version":    "0.2",
			"agent":               map[string]any{"name": "conformance-agent", "version": "0.2.0"},
			"max_concurrent_runs": 1,
		})
		if connection.Write(request.Context(), websocket.MessageText, ack) != nil {
			return
		}
		_, data, err = connection.Read(request.Context())
		if err != nil {
			return
		}
		var received map[string]any
		if json.Unmarshal(data, &received) != nil {
			return
		}
		result, err := handler(received)
		if err != nil {
			return
		}
		data, _ = json.Marshal(result)
		_ = connection.Write(request.Context(), websocket.MessageText, data)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	connection, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return Result{}, err
	}
	defer connection.Close(websocket.StatusNormalClosure, "")
	connection.SetReadLimit(maxFrameSize)

	handshake, _ := json.Marshal(map[string]any{
		"type":               "handshake",
		"supported_versions": []string{"0.2"},
		"client":             map[string]any{"name": "hap-conformance", "version": "0.2.0"},
	})
	if err := connection.Write(ctx, websocket.MessageText, handshake); err != nil {
		return Result{}, err
	}
	_, ack, err := connection.Read(ctx)
	if err != nil {
		return Result{}, err
	}
	var ackMessage map[string]any
	if json.Unmarshal(ack, &ackMessage) != nil || ackMessage["type"] != "handshake_ack" {
		return Result{}, errors.New("invalid websocket handshake acknowledgement")
	}
	data, _ := json.Marshal(task)
	if err := connection.Write(ctx, websocket.MessageText, data); err != nil {
		return Result{}, err
	}
	_, data, err = connection.Read(ctx)
	if err != nil {
		return Result{}, err
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}
