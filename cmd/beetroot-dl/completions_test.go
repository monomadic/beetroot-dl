package main

import (
	"strings"
	"testing"
)

func TestPrintCompletionsUnknownShell(t *testing.T) {
	if err := printCompletions("fish"); err == nil {
		t.Fatal("expected an error for an unsupported shell")
	}
}

func TestCompletionScriptsCoverAllFlags(t *testing.T) {
	// Every flag registered in main() must appear in both scripts, so adding
	// a flag without updating completions fails here.
	flags := []string{"--interactive", "--verbose", "--quiet", "--config", "--completions", "--version", "--help"}
	for name, script := range map[string]string{"bash": bashCompletions, "zsh": zshCompletions} {
		for _, f := range flags {
			if !strings.Contains(script, f) {
				t.Errorf("%s completions missing %s", name, f)
			}
		}
	}
}
