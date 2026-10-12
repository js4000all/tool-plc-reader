package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	previousVersion := version
	version = "test-version"
	t.Cleanup(func() { version = previousVersion })

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := run([]string{"-version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got, want := stdout.String(), "test-version\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunRejectsUnexpectedArgument(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := run([]string{"unexpected"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run() exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "does not accept positional arguments") {
		t.Errorf("stderr = %q, want positional-argument error", got)
	}
}
