package portalclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LogHandler receives every plain `data:` line from the SSE stream.
type LogHandler func(line string)

// StageHandler receives the `current_stage` value emitted by `event: stage`.
type StageHandler func(stage string)

// DoneHandler receives the parsed `event: done` payload exactly once. After
// it returns, FollowExecution returns nil.
type DoneHandler func(event DoneEvent)

const (
	// followMaxAttempts caps reconnection retries on transport errors. The
	// server's planned `event: reconnect` is NOT counted against this cap —
	// it is a graceful refresh of the long-poll, not a failure.
	followMaxAttempts = 3
	// followStreamTimeout is the per-stream request timeout. The server is
	// expected to roll over its own long-poll inside this window, so a
	// shorter cap would race with the planned 120s reconnect cadence.
	followStreamTimeout = 5 * time.Minute
)

// followBackoffSchedule is the wait between transport-error retries. We use
// 1s/2s/4s exponential to ride out transient edge-runtime restarts without
// hammering Supabase.
var followBackoffSchedule = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
}

// errReconnect signals the streamSSE loop that the server emitted
// `event: reconnect` and the caller should re-issue the GET with the new
// cursor. Internal sentinel; never surfaced to callers.
var errReconnect = errors.New("portalclient: sse reconnect requested")

// errStreamDone signals that the server emitted `event: done`. The caller
// returns nil; this distinguishes "natural end" from "transport failure".
var errStreamDone = errors.New("portalclient: sse stream done")

// FollowExecution opens an SSE long-poll against /functions/v1/execution-logs
// and dispatches each event to the supplied handlers. It transparently
// reconnects when:
//
//   - The server emits `event: reconnect` (planned ~120s rollover): no
//     attempt counter increment, cursor advances to event payload value.
//   - The HTTP call fails with a transport error: counted against
//     followMaxAttempts with backoff per followBackoffSchedule.
//
// FollowExecution returns:
//
//   - nil when the server emits `event: done` (the Done handler has run).
//   - ctx.Err() when the caller cancels (typically Ctrl+C).
//   - the underlying transport error after followMaxAttempts.
//
// Any handler may be nil; the corresponding events are silently dropped.
// fromSeq is the initial cursor; pass 0 to receive the full log from the
// beginning of the execution.
func (c *PortalClient) FollowExecution(
	ctx context.Context,
	executionID string,
	fromSeq int64,
	onLine LogHandler,
	onStage StageHandler,
	onDone DoneHandler,
) error {
	if executionID == "" {
		return errors.New("portalclient: follow execution: execution_id is required")
	}
	if c.tokenStore == nil {
		return errors.New("portalclient: follow execution: token store is not configured")
	}

	cursor := fromSeq
	attempts := 0

	for {
		newCursor, err := c.streamSSE(ctx, executionID, cursor, onLine, onStage, onDone)
		switch {
		case err == nil, errors.Is(err, errStreamDone):
			return nil
		case errors.Is(err, errReconnect):
			cursor = newCursor
			attempts = 0
			continue
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return ctx.Err()
		}

		// Transport-level failure: count against the retry cap.
		attempts++
		if attempts >= followMaxAttempts {
			return fmt.Errorf("portalclient: follow execution: %w", err)
		}

		wait := followBackoffSchedule[attempts-1]
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		// Resume from whatever cursor we last advanced to (newCursor may be
		// zero if streamSSE failed before reading any line; that's fine —
		// the server is idempotent on from_seq).
		if newCursor > cursor {
			cursor = newCursor
		}
	}
}

// streamSSE opens a single GET against execution-logs and parses events
// until it returns. The returned cursor is the highest seq observed (used
// to resume the next attempt without losing or re-replaying lines). The
// returned error is one of: nil (stream ended cleanly without `done`),
// errStreamDone (handler invoked), errReconnect (caller must re-open),
// or a transport / decode error.
func (c *PortalClient) streamSSE(
	ctx context.Context,
	executionID string,
	cursor int64,
	onLine LogHandler,
	onStage StageHandler,
	onDone DoneHandler,
) (int64, error) {
	token, err := c.tokenStore.Load()
	if err != nil {
		return cursor, err
	}

	url := fmt.Sprintf("%s/functions/v1/execution-logs?execution_id=%s&from_seq=%d",
		c.baseURL, executionID, cursor)

	streamCtx, cancel := context.WithTimeout(ctx, followStreamTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, url, nil)
	if err != nil {
		return cursor, fmt.Errorf("build sse request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return cursor, fmt.Errorf("open sse stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return cursor, classifyError("execution-logs", resp)
	}

	return parseSSE(resp.Body, cursor, onLine, onStage, onDone)
}

// parseSSE drains the stream applying the SSE wire format described in
// §6.8: `data:` lines (default = log entry), `event: <name>` followed by a
// `data: <json>` line for `stage`, `heartbeat`, `reconnect`, `done`. Blank
// lines mark message boundaries. Extracted to be unit-testable without an
// httptest server.
func parseSSE(
	body io.Reader,
	cursor int64,
	onLine LogHandler,
	onStage StageHandler,
	onDone DoneHandler,
) (int64, error) {
	scanner := bufio.NewScanner(body)
	// The plan caps a single line at ~64KB; allow some headroom for SSE
	// envelope characters.
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	var (
		eventType string
		dataBuf   strings.Builder
	)

	dispatch := func() error {
		if eventType == "" && dataBuf.Len() == 0 {
			return nil
		}
		raw := dataBuf.String()
		dataBuf.Reset()
		eventName := eventType
		eventType = ""

		switch eventName {
		case "", "message":
			if onLine != nil && raw != "" {
				onLine(raw)
			}
		case "stage":
			var ev stageEvent
			if err := json.Unmarshal([]byte(raw), &ev); err == nil && onStage != nil && ev.CurrentStage != "" {
				onStage(ev.CurrentStage)
			}
		case "heartbeat":
			// no-op, only used to keep the connection alive.
		case "reconnect":
			var ev reconnectEvent
			if err := json.Unmarshal([]byte(raw), &ev); err != nil {
				return fmt.Errorf("decode reconnect event: %w", err)
			}
			if ev.FromSeq > cursor {
				cursor = ev.FromSeq
			}
			return errReconnect
		case "done":
			var ev DoneEvent
			if err := json.Unmarshal([]byte(raw), &ev); err != nil {
				return fmt.Errorf("decode done event: %w", err)
			}
			if onDone != nil {
				onDone(ev)
			}
			return errStreamDone
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return cursor, err
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, ":"):
			// SSE comment / keep-alive — ignore.
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			payload := strings.TrimPrefix(line, "data:")
			payload = strings.TrimPrefix(payload, " ")
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(payload)
		case strings.HasPrefix(line, "id:"):
			// SSE last-event-id; not used by the portal contract today.
		default:
			// Unknown field — per SSE spec, ignore silently.
		}
	}
	if err := scanner.Err(); err != nil {
		return cursor, fmt.Errorf("read sse stream: %w", err)
	}
	// Final dispatch in case the stream ended without a terminating blank
	// line. Standard SSE servers always end the message with \n\n, but
	// being defensive avoids losing the last event.
	if err := dispatch(); err != nil {
		return cursor, err
	}
	return cursor, nil
}
