package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Event struct {
	Event  string `json:"event"`
	Ts     string `json:"ts"`
	Status string `json:"status,omitempty"`
	Uptime int    `json:"uptime,omitempty"`
	TaskID string `json:"task_id,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func emit(e Event) {
	e.Ts = time.Now().UTC().Format(time.RFC3339)
	data, _ := json.Marshal(e)
	fmt.Println(string(data))
}

func main() {
	log.SetOutput(os.Stderr)

	url := os.Getenv("URL_TO_CHECK")
	if url == "" {
		url = "https://example.com"
		log.Println("URL_TO_CHECK not set, defaulting to", url)
	}

	start := time.Now()
	emit(Event{Event: "agent.started"})
	emit(Event{Event: "agent.ready"})

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	task := time.NewTicker(30 * time.Second)
	defer task.Stop()

	taskNum := 0
	check := func() {
		taskNum++
		tid := fmt.Sprintf("t-%03d", taskNum)
		emit(Event{Event: "task.started", TaskID: tid, Detail: url})
		client := &http.Client{Timeout: 10 * time.Second}
		t0 := time.Now()
		resp, err := client.Get(url)
		ms := time.Since(t0).Milliseconds()
		if err != nil {
			emit(Event{Event: "task.failed", TaskID: tid, Detail: err.Error()})
			return
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			emit(Event{Event: "task.completed", TaskID: tid, Detail: fmt.Sprintf("status=%d time=%dms", resp.StatusCode, ms)})
		} else {
			emit(Event{Event: "task.failed", TaskID: tid, Detail: fmt.Sprintf("status=%d", resp.StatusCode)})
		}
	}

	check()

	for {
		select {
		case <-heartbeat.C:
			emit(Event{Event: "agent.heartbeat", Status: "healthy", Uptime: int(time.Since(start).Seconds())})
		case <-task.C:
			check()
		case <-sig:
			emit(Event{Event: "agent.stopped"})
			os.Exit(0)
		}
	}
}
