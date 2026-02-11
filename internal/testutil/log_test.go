package testutil_test

import (
	"log"
	"strings"
	"testing"

	"github.com/r6m/tlrpc/internal/testutil"
)

func TestLogCapture(t *testing.T) {
	capture := testutil.NewLogCapture(t)
	defer capture.Close()

	// Test initial state
	if capture.String() != "" {
		t.Errorf("New LogCapture should start empty, got: %q", capture.String())
	}

	// Test logging
	log.Println("test message 1")
	log.Printf("test message %d", 2)

	output := capture.String()
	if !strings.Contains(output, "test message 1") {
		t.Errorf("LogCapture should contain 'test message 1', got: %q", output)
	}
	if !strings.Contains(output, "test message 2") {
		t.Errorf("LogCapture should contain 'test message 2', got: %q", output)
	}
}

func TestLogCapture_Lines(t *testing.T) {
	capture := testutil.NewLogCapture(t)
	defer capture.Close()

	// Test empty capture
	lines := capture.Lines()
	if len(lines) != 0 {
		t.Errorf("Empty LogCapture should have 0 lines, got: %d", len(lines))
	}

	// Test with content
	log.Println("line 1")
	log.Println("line 2")
	log.Println("line 3")

	lines = capture.Lines()
	if len(lines) != 3 {
		t.Errorf("LogCapture should have 3 lines, got: %d", len(lines))
	}

	expected := []string{"line 1", "line 2", "line 3"}
	for i, line := range lines {
		if line != expected[i] {
			t.Errorf("Line %d = %q; want %q", i, line, expected[i])
		}
	}
}

func TestLogCapture_Contains(t *testing.T) {
	capture := testutil.NewLogCapture(t)
	defer capture.Close()

	log.Println("hello world")

	if !capture.Contains("hello") {
		t.Error("LogCapture should contain 'hello'")
	}
	if !capture.Contains("world") {
		t.Error("LogCapture should contain 'world'")
	}
	if capture.Contains("goodbye") {
		t.Error("LogCapture should not contain 'goodbye'")
	}
}

func TestLogCapture_Reset(t *testing.T) {
	capture := testutil.NewLogCapture(t)
	defer capture.Close()

	log.Println("before reset")
	if capture.String() == "" {
		t.Error("LogCapture should have content before reset")
	}

	capture.Reset()
	if capture.String() != "" {
		t.Errorf("LogCapture should be empty after reset, got: %q", capture.String())
	}

	log.Println("after reset")
	if capture.String() == "" {
		t.Error("LogCapture should have content after reset")
	}
}

func TestLogCapture_Logger(t *testing.T) {
	capture := testutil.NewLogCapture(t)
	defer capture.Close()

	logger := capture.Logger()
	logger.Println("custom logger message")

	if !capture.Contains("custom logger message") {
		t.Error("LogCapture should contain message from custom logger")
	}
}