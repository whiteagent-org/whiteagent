package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// recordingAPI records Telegram API method calls made through httptest.
type recordingAPI struct {
	mu    sync.Mutex
	calls []apiCall
}

type apiCall struct {
	method string
	params map[string]any
}

func (r *recordingAPI) record(method string, params map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, apiCall{method: method, params: params})
}

func (r *recordingAPI) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// newRecordingAPIClient creates an apiClient backed by an httptest server
// that records all API method calls and returns a success response.
func newRecordingAPIClient(rec *recordingAPI) *apiClient {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract method from URL path: /bot<token>/<method>
		// Path format: /bottest-token/<method>
		path := r.URL.Path
		// Find last slash to get method name.
		var method string
		for i := len(path) - 1; i >= 0; i-- {
			if path[i] == '/' {
				method = path[i+1:]
				break
			}
		}

		var params map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&params)
		}

		rec.record(method, params)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))

	return &apiClient{
		baseURL:    srv.URL,
		token:      "test-token",
		httpClient: srv.Client(),
	}
}

func TestIndicateImmediateTyping(t *testing.T) {
	rec := &recordingAPI{}
	p := &Plugin{
		api: newRecordingAPIClient(rec),
	}

	stop := p.Indicate(context.Background(), map[string]string{"chat_id": "12345"})
	defer stop()

	// Immediate call should have happened synchronously.
	if rec.count() < 1 {
		t.Fatal("expected at least one sendChatAction call immediately")
	}

	rec.mu.Lock()
	r := rec.calls[0]
	rec.mu.Unlock()

	if r.method != "sendChatAction" {
		t.Errorf("expected method sendChatAction, got %s", r.method)
	}
	if r.params["chat_id"] != "12345" {
		t.Errorf("expected chat_id 12345, got %v", r.params["chat_id"])
	}
	if r.params["action"] != "typing" {
		t.Errorf("expected action typing, got %v", r.params["action"])
	}
}

func TestIndicateStopTerminatesGoroutine(t *testing.T) {
	rec := &recordingAPI{}
	p := &Plugin{
		api: newRecordingAPIClient(rec),
	}

	stop := p.Indicate(context.Background(), map[string]string{"chat_id": "12345"})

	// Let ticker potentially fire (not guaranteed in short time).
	time.Sleep(50 * time.Millisecond)
	stop()

	// Give cancel call time to complete.
	time.Sleep(50 * time.Millisecond)

	// Record count after stop (includes cancel action).
	countAfterStop := rec.count()

	// Wait to confirm no more calls arrive.
	time.Sleep(100 * time.Millisecond)
	countLater := rec.count()

	if countLater != countAfterStop {
		t.Errorf("expected no new calls after stop, got %d -> %d", countAfterStop, countLater)
	}
}

func TestIndicateStopSendsCancelAction(t *testing.T) {
	rec := &recordingAPI{}
	p := &Plugin{
		api: newRecordingAPIClient(rec),
	}

	stop := p.Indicate(context.Background(), map[string]string{"chat_id": "12345"})
	stop()

	// Give cancel call time to complete.
	time.Sleep(50 * time.Millisecond)

	rec.mu.Lock()
	defer rec.mu.Unlock()

	// Should have at least 2 calls: initial typing + cancel.
	if len(rec.calls) < 2 {
		t.Fatalf("expected at least 2 calls (typing + cancel), got %d", len(rec.calls))
	}

	last := rec.calls[len(rec.calls)-1]
	if last.method != "sendChatAction" {
		t.Errorf("expected last call to be sendChatAction, got %s", last.method)
	}
	if last.params["action"] != "cancel" {
		t.Errorf("expected cancel action, got %v", last.params["action"])
	}
	if last.params["chat_id"] != "12345" {
		t.Errorf("expected chat_id 12345, got %v", last.params["chat_id"])
	}
}

func TestIndicateStopIdempotent(t *testing.T) {
	rec := &recordingAPI{}
	p := &Plugin{
		api: newRecordingAPIClient(rec),
	}

	stop := p.Indicate(context.Background(), map[string]string{"chat_id": "12345"})

	// Calling stop twice must not panic.
	stop()
	stop()
}

func TestIndicateTickerRunning(t *testing.T) {
	rec := &recordingAPI{}
	p := &Plugin{
		api: newRecordingAPIClient(rec),
	}

	stop := p.Indicate(context.Background(), map[string]string{"chat_id": "12345"})
	defer stop()

	// The immediate call should be present.
	if rec.count() < 1 {
		t.Fatal("expected immediate typing call")
	}

	// Verify the goroutine is alive by checking stop terminates it.
	// (We can't easily test the 4s interval in unit tests without time mocking,
	// but we verify the goroutine exists and responds to stop.)
	stop()
	time.Sleep(50 * time.Millisecond)
	finalCount := rec.count()

	// After stop, count should be stable.
	time.Sleep(50 * time.Millisecond)
	if rec.count() != finalCount {
		t.Error("goroutine still running after stop")
	}
}
