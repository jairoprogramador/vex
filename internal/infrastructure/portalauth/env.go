package portalauth

// Build-time configuration. Committed with empty values; GoReleaser injects
// production values via -ldflags at release time (see goreleaser.yaml).
//
// Local dev setup (run once after cloning):
//
//	git update-index --skip-worktree vex/internal/infrastructure/portalauth/env.go
//
// Then edit this file with your dev values — git will not track the changes.
var (
	defaultPortalURL     = ""
	defaultBackendURL     = ""
	defaultBackendAnonKey = ""
)
