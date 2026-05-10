package portalauth

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser launches the user's default browser pointing to url. It is a
// best-effort operation: if the platform-specific command is missing or
// fails, the error is returned so the caller can decide to fall back to
// printing the URL on stdout. The function does not wait for the browser
// process to finish.
func OpenBrowser(url string) error {
	var (
		bin  string
		args []string
	)

	switch runtime.GOOS {
	case "darwin":
		bin = "open"
		args = []string{url}
	case "linux":
		bin = "xdg-open"
		args = []string{url}
	case "windows":
		bin = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("open browser: unsupported platform %q", runtime.GOOS)
	}

	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser via %s: %w", bin, err)
	}
	// Detach: we don't care about the exit status of the launcher process.
	go func() { _ = cmd.Wait() }()
	return nil
}
