package cmd

import (
	"strings"
	"testing"

	"github.com/local/tutti/internal/api"
)

func TestRunToken(t *testing.T) {
	out, err := captureStdout(t, func() error {
		rootCmd.SetArgs([]string{"token", "hello world"})
		return rootCmd.Execute()
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Token:") {
		t.Errorf("expected Token line, got: %s", out)
	}
	if !strings.Contains(out, "https://www.tutti.ch/de/q/suche/") {
		t.Errorf("expected URL line, got: %s", out)
	}
	// The token printed should match what api.BuildToken returns.
	want := api.BuildToken("hello world")
	if !strings.Contains(out, want) {
		t.Errorf("printed token mismatch: want substring %q, got:\n%s", want, out)
	}
}
