package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestHandleCLIArgs_HelpPrintsVersion(t *testing.T) {
	output := captureStdout(t, func() {
		handled := handleCLIArgs([]string{"help"})
		if !handled {
			t.Fatal("expected help arg to be handled")
		}
	})

	expected := "better_diff " + appVersion + "\n"
	if output != expected {
		t.Fatalf("unexpected help output: got %q want %q", output, expected)
	}
}

func TestHandleCLIArgs_DiffHelpPrintsVersion(t *testing.T) {
	output := captureStdout(t, func() {
		handled := handleCLIArgs([]string{"diff", "help"})
		if !handled {
			t.Fatal("expected diff help args to be handled")
		}
	})

	expected := "better_diff " + appVersion + "\n"
	if output != expected {
		t.Fatalf("unexpected help output: got %q want %q", output, expected)
	}
}

func TestHandleCLIArgs_NoArgsNotHandled(t *testing.T) {
	if handled := handleCLIArgs(nil); handled {
		t.Fatal("expected nil args to not be handled")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed creating pipe: %v", err)
	}

	os.Stdout = w
	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed closing writer: %v", err)
	}
	os.Stdout = original

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed reading output: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("failed closing reader: %v", err)
	}

	return buf.String()
}
