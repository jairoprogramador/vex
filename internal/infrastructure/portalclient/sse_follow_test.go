package portalclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// flushingHandler writes the supplied SSE payload to the response and
// flushes it incrementally. It supports a slice of "chunks" that are
// flushed in order, optionally with a delay before each chunk.
type flushingHandler struct {
	chunks []sseChunk
}

type sseChunk struct {
	delay time.Duration
	data  string
}

func (h *flushingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	for _, ch := range h.chunks {
		if ch.delay > 0 {
			select {
			case <-time.After(ch.delay):
			case <-r.Context().Done():
				return
			}
		}
		_, _ = w.Write([]byte(ch.data))
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func TestFollowExecution_ReceivesLinesAndDone(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(&flushingHandler{
		chunks: []sseChunk{
			{data: "data: line one\n\n"},
			{data: "data: line two\n\n"},
			{data: "event: stage\ndata: {\"current_stage\":\"running_step:deploy\"}\n\n"},
			{data: "event: done\ndata: {\"status\":\"succeeded\",\"exit_code\":0,\"logs_lost\":false,\"current_stage\":\"running_step:deploy\"}\n\n"},
		},
	})
	defer srv.Close()

	client := NewPortalClient(srv.URL, newTestStore(t), srv.Client())

	var (
		lines  []string
		stages []string
		done   DoneEvent
	)
	err := client.FollowExecution(context.Background(), "exec-1", 0,
		func(line string) { lines = append(lines, line) },
		func(stage string) { stages = append(stages, stage) },
		func(d DoneEvent) { done = d },
	)
	if err != nil {
		t.Fatalf("follow: %v", err)
	}
	if len(lines) != 2 || lines[0] != "line one" || lines[1] != "line two" {
		t.Fatalf("lines: got %+v", lines)
	}
	if len(stages) != 1 || stages[0] != "running_step:deploy" {
		t.Fatalf("stages: got %+v", stages)
	}
	if done.Status != "succeeded" || done.ExitCode != 0 || done.CurrentStage != "running_step:deploy" {
		t.Fatalf("done event: got %+v", done)
	}
}

func TestFollowExecution_ReconnectAdvancesCursor(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/functions/v1/cli-execution-logs", func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		fromSeq := r.URL.Query().Get("from_seq")
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		switch n {
		case 1:
			if fromSeq != "0" {
				t.Errorf("first call from_seq: got %q, want 0", fromSeq)
			}
			_, _ = fmt.Fprint(w, "data: pre\n\nevent: reconnect\ndata: {\"from_seq\":42}\n\n")
		case 2:
			if fromSeq != "42" {
				t.Errorf("second call from_seq: got %q, want 42", fromSeq)
			}
			_, _ = fmt.Fprint(w, "data: post\n\nevent: done\ndata: {\"status\":\"succeeded\",\"exit_code\":0,\"logs_lost\":false}\n\n")
		default:
			t.Errorf("unexpected extra call #%d", n)
		}
		if flusher != nil {
			flusher.Flush()
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewPortalClient(srv.URL, newTestStore(t), srv.Client())

	var lines []string
	err := client.FollowExecution(context.Background(), "exec-1", 0,
		func(line string) { lines = append(lines, line) },
		nil,
		func(DoneEvent) {},
	)
	if err != nil {
		t.Fatalf("follow: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 server calls (initial + reconnect), got %d", calls.Load())
	}
	if len(lines) != 2 || lines[0] != "pre" || lines[1] != "post" {
		t.Fatalf("lines: got %+v", lines)
	}
}

func TestFollowExecution_ReportsFailedStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(&flushingHandler{
		chunks: []sseChunk{
			{data: "data: error trace\n\n"},
			{data: "event: done\ndata: {\"status\":\"failed\",\"exit_code\":2,\"logs_lost\":true,\"current_stage\":\"running_step:test\"}\n\n"},
		},
	})
	defer srv.Close()

	client := NewPortalClient(srv.URL, newTestStore(t), srv.Client())

	var done DoneEvent
	err := client.FollowExecution(context.Background(), "exec-1", 0,
		nil, nil,
		func(d DoneEvent) { done = d },
	)
	if err != nil {
		t.Fatalf("follow: %v", err)
	}
	if done.Status != "failed" || done.ExitCode != 2 || !done.LogsLost {
		t.Fatalf("done: got %+v", done)
	}
}

func TestFollowExecution_PropagatesContextCancel(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-hold:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(hold)

	client := NewPortalClient(srv.URL, newTestStore(t), srv.Client())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := client.FollowExecution(ctx, "exec-1", 0, nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestFollowExecution_RetriesAndGivesUp(t *testing.T) {
	t.Parallel()

	prevSchedule := followBackoffSchedule
	followBackoffSchedule = []time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond}
	defer func() { followBackoffSchedule = prevSchedule }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	client := NewPortalClient(srv.URL, newTestStore(t), srv.Client())

	err := client.FollowExecution(context.Background(), "exec-1", 0, nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error after retries exhausted, got nil")
	}
}

func TestParseSSE_HeartbeatIsNoop(t *testing.T) {
	t.Parallel()

	body := strings.NewReader("event: heartbeat\ndata: {\"seq\":42}\n\nevent: done\ndata: {\"status\":\"succeeded\",\"exit_code\":0,\"logs_lost\":false}\n\n")
	called := false
	_, err := parseSSE(body, 0, nil, nil, func(DoneEvent) { called = true })
	if !errors.Is(err, errStreamDone) {
		t.Fatalf("want errStreamDone, got %v", err)
	}
	if !called {
		t.Fatalf("done handler was not called")
	}
}

func TestParseSSE_IgnoresUnknownFields(t *testing.T) {
	t.Parallel()

	body := strings.NewReader(": comment line\nid: 1\nfoo: bar\ndata: hello\n\nevent: done\ndata: {\"status\":\"succeeded\",\"exit_code\":0,\"logs_lost\":false}\n\n")
	var got string
	_, err := parseSSE(body, 0, func(line string) { got = line }, nil, func(DoneEvent) {})
	if !errors.Is(err, errStreamDone) {
		t.Fatalf("want errStreamDone, got %v", err)
	}
	if got != "hello" {
		t.Fatalf("expected log %q, got %q", "hello", got)
	}
}
