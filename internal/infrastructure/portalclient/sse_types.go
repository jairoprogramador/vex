package portalclient

// DoneEvent is the body of the `event: done` server-sent event emitted by
// the `cli-execution-logs` long-poll endpoint when the execution reaches a
// terminal state (§6.8 plan_deploy.md). The CLI uses it to print the final
// status and decide the process exit code.
type DoneEvent struct {
	// Status is one of `succeeded`, `failed`, `canceled`, `error`.
	Status string `json:"status"`
	// ExitCode is the underlying pipeline exit code reported by `vexd run`.
	// 0 on succeeded, non-zero on failed/error.
	ExitCode int `json:"exit_code"`
	// LogsLost is true when the SupabaseLogObserver inside `vexd run` had to
	// drop a batch (network failure on the log-ingest endpoint). The CLI
	// surfaces this as a warning so users know the streamed output may be
	// incomplete even when status is `succeeded`.
	LogsLost bool `json:"logs_lost"`
	// CurrentStage is the last reported pipeline stage. Optional; useful
	// when status != succeeded so users see where it failed.
	CurrentStage string `json:"current_stage,omitempty"`
}

// reconnectEvent is the body of the `event: reconnect` SSE message: the
// server hits a long-poll cap (~120s) and signals the client to re-open the
// connection from the cursor it provides. Internal to the package.
type reconnectEvent struct {
	FromSeq int64 `json:"from_seq"`
}

// stageEvent is the body of the `event: stage` SSE message: the server
// detected a `current_stage` change in the executions row. Internal.
type stageEvent struct {
	CurrentStage string `json:"current_stage"`
}
