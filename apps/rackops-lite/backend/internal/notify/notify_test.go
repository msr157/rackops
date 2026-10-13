package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestColorFor(t *testing.T) {
	cases := []struct {
		level    string
		expected int
	}{
		{"critical", 0xf87171},
		{"failed", 0xf87171},
		{"warning", 0xfbbf24},
		{"info", 0x34d399},
		{"nominal", 0x34d399},
		{"unknown", 0x34d399},
	}

	for _, tc := range cases {
		c := colorFor(tc.level)
		if c != tc.expected {
			t.Errorf("colorFor(%q) = 0x%x; expected 0x%x", tc.level, c, tc.expected)
		}
	}
}

func TestSendJSON_MockServer(t *testing.T) {
	var receivedPayload map[string]any
	var contentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	payload := map[string]any{
		"message": "test-payload",
		"code":    123,
	}

	sendJSON(server.URL, payload)

	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}
	if receivedPayload["message"] != "test-payload" || receivedPayload["code"] != float64(123) {
		t.Errorf("unexpected payload received: %+v", receivedPayload)
	}

	// Invalid URL test should not panic
	sendJSON("://invalid-url", payload)
}

func TestSend_Fanout(t *testing.T) {
	var wg sync.WaitGroup
	var discordReceived, slackReceived, genericReceived bool
	var mu sync.Mutex

	wg.Add(3)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := r.URL.Path
		switch path {
		case "/discord":
			discordReceived = true
			wg.Done()
		case "/slack":
			slackReceived = true
			wg.Done()
		case "/generic":
			genericReceived = true
			wg.Done()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	targets := Targets{
		DiscordWebhook: mockServer.URL + "/discord",
		SlackWebhook:   mockServer.URL + "/slack",
		GenericWebhook: mockServer.URL + "/generic",
	}

	Send(targets, "Test Alert", "Everything is nominal", "info")

	// Wait for goroutines to deliver
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification fanout")
	}

	mu.Lock()
	defer mu.Unlock()
	if !discordReceived || !slackReceived || !genericReceived {
		t.Errorf("fanout delivery missing: discord=%v, slack=%v, generic=%v",
			discordReceived, slackReceived, genericReceived)
	}
}
