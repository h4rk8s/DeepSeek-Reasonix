package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestRawContextLimitResponseInstallsChunkedProjection(t *testing.T) {
	var mu sync.Mutex
	var requestBodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		mu.Lock()
		requestBodies = append(requestBodies, append([]byte(nil), body...))
		call := len(requestBodies)
		mu.Unlock()

		if call <= 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"This model's maximum context length is 1048576 tokens. However, you requested 1240242 tokens (1232050 in the messages, 8192 in the completion).","type":"invalid_request_error","code":"invalid_request_error"}}`)
			return
		}
		writeSSE(w, t, streamChunk(deltaText(fmt.Sprintf("wire digest %d", call))), finishChunk("stop"))
	}))
	defer srv.Close()

	a, _ := newAgent(t, srv.URL, tool.NewRegistry(), 32_000, 2)
	for i := range 8 {
		a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("wire task %d %s", i, strings.Repeat("context ", 800))})
		a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("wire answer %d %s", i, strings.Repeat("result ", 800))})
	}
	before := a.sess.conversation.Snapshot()

	_, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{
		Trigger:              CompactionTriggerManual,
		Force:                true,
		AllowChunkedFallback: true,
	})
	if err != nil {
		t.Fatalf("raw context-limit recovery: %v", err)
	}
	mu.Lock()
	requestCount := len(requestBodies)
	mu.Unlock()
	if requestCount != 3 {
		t.Fatalf("HTTP requests = %d, want raw and slim failures followed by one chunked summary", requestCount)
	}
	if got := a.currentProjectionVersion(); got != 1 {
		t.Fatalf("projection version = %d, want 1", got)
	}
	if after := a.sess.conversation.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("context recovery mutated the canonical transcript")
	}
}
