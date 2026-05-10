package application

import "context"

// Runner is the small contract every deploy-orchestrator implements. The
// CLI root command depends only on this interface; the factory decides at
// runtime whether to hand back a local Docker executor or the remote
// portal-driven one based on the `--remote` flag.
//
// Step is the pipeline phase (`test`, `supply`, `package`, `deploy`...);
// environment is the target lane (`prod`, `stag`, `sand`...). Both are
// surfaced verbatim from `vex <step> [env]` argv.
type Runner interface {
	Run(ctx context.Context, step, environment string) error
}
