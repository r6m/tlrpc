package testutil

import (
	"bytes"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
)

// LogCapture captures log output for testing purposes.
type LogCapture struct {
	mu     sync.Mutex
	buf    *bytes.Buffer
	oldOut io.Writer
	logger *log.Logger
}

// NewLogCapture creates a new log capture that redirects log output.
// Call Close() when done to restore original output.
func NewLogCapture(t testing.TB) *LogCapture {
	t.Helper()
	buf := &bytes.Buffer{}

	// Save current logger state
	oldFlags := log.Flags()
	oldOut := log.Writer()

	capture := &LogCapture{
		buf:    buf,
		oldOut: oldOut,
		logger: log.New(buf, "", 0), // No prefix or timestamp for captured logs
	}

	// Redirect default logger to our buffer with no formatting
	log.SetOutput(buf)
	log.SetFlags(0)

	t.Cleanup(func() {
		capture.Close()
		log.SetFlags(oldFlags) // Restore original flags
	})

	return capture
}

// Close restores the original log output.
func (c *LogCapture) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.oldOut != nil {
		log.SetOutput(c.oldOut)
		c.oldOut = nil
	}
}

// String returns the captured log output as a string.
func (c *LogCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Lines returns the captured log output split into lines.
func (c *LogCapture) Lines() []string {
	str := c.String()
	if str == "" {
		return nil
	}
	// Remove trailing newline if present
	str = strings.TrimSuffix(str, "\n")
	return strings.Split(str, "\n")
}

// Contains checks if the captured output contains the given substring.
func (c *LogCapture) Contains(substring string) bool {
	return strings.Contains(c.String(), substring)
}

// Reset clears the captured log output.
func (c *LogCapture) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.Reset()
}

// Logger returns a logger that writes to this capture.
func (c *LogCapture) Logger() *log.Logger {
	return c.logger
}
