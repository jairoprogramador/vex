package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestFormatExpiry(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	got, ok := formatExpiry("2026-11-18T12:00:00Z", now)
	if !ok || !strings.HasSuffix(got, "(30 días)") {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if got, _ := formatExpiry("2026-10-01T00:00:00Z", now); !strings.HasSuffix(got, "(0 días)") {
		t.Fatalf("past expiry must clamp to 0 days, got %q", got)
	}
	if _, ok := formatExpiry("", now); ok {
		t.Fatal("empty date must not be ok")
	}
}
