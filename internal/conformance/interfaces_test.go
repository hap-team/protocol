package conformance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestInterfaceEquivalence(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "conformance", "scenarios", "success.json"))
	if err != nil {
		t.Fatal(err)
	}
	var scenario Scenario
	if err := json.Unmarshal(data, &scenario); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var baseline Result
	for _, interfaceType := range []string{"process", "http", "websocket", "a2a"} {
		interfaceType := interfaceType
		t.Run(interfaceType, func(t *testing.T) {
			result, err := RunScenario(ctx, interfaceType, scenario)
			if err != nil {
				t.Fatal(err)
			}
			if baseline.Type == "" {
				baseline = result
				return
			}
			if !reflect.DeepEqual(result, baseline) {
				t.Fatalf("%s result differs:\nwant: %#v\n got: %#v", interfaceType, baseline, result)
			}
		})
	}
}

func TestInterfaceCancellationModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"supported", "best_effort", "unsupported"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			transcript, err := RunCancellationScenario(context.Background(), mode)
			if err != nil {
				t.Fatal(err)
			}
			if transcript.Acknowledgement.RunID != transcript.Request.RunID {
				t.Fatal("cancellation acknowledgement changed run_id")
			}
			if mode == "unsupported" && transcript.Acknowledgement.Accepted {
				t.Fatal("unsupported cancellation was accepted")
			}
			if mode != "unsupported" && !transcript.Acknowledgement.Accepted {
				t.Fatalf("%s cancellation was not accepted", mode)
			}
		})
	}
}

func TestInterfaceFailureGuards(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"malformed-framing",
		"stdout-contamination",
		"reconnect-sequence",
		"http-polling-fallback",
		"multiple-terminal-results",
		"oversized-frame",
		"cross-host-redirect",
		"transport-loss-unknown-outcome",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			if err := VerifyInterfaceGuard(context.Background(), name); err != nil {
				t.Fatal(err)
			}
		})
	}
}
