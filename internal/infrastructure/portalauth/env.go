package portalauth

// Build-time configuration. Committed with empty values; GoReleaser injects
// production values via -ldflags at release time (see goreleaser.yaml).
//
// Local dev setup (run once after cloning):
//
//     git update-index --skip-worktree vex/internal/infrastructure/portalauth/env.go
//
// Then edit this file with your dev values — git will not track the changes.

// - Si en el futuro se necesita forzar un push del archivo (ej. cambio de variable), se revierte con:
// git update-index --no-skip-worktree internal/infrastructure/portalauth/env.go
// - El flag es local por developer; cada colaborador debe correr el comando tras clonar.

var (
	defaultPortalURL  = ""
	defaultBackendURL = ""
	defaultClientID   = ""
)
