package application

import "context"

// Runner is the small contract every deploy-orchestrator implements. The
// CLI root command depends only on this interface; the factory decides at
// runtime whether to hand back the local executor (vex-engine in a Docker
// container) or the remote portal-driven one, based on the resolved execution
// mode (`--mode`, vexconfig.yaml, or the user/global config).
//
// Step is the pipeline phase (`test`, `supply`, `package`, `deploy`...);
// environment is the target lane (`prod`, `stag`, `sand`...). Both are
// surfaced verbatim from `vex <step> [env]` argv.
type Runner interface {
	Run(ctx context.Context, step, environment string) error
}
