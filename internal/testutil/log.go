// Package testutil provides testing utilities.
package testutil

import (
	"bytes"
	"io"
	"log"
)

// LogCapture captures standard logger output.
type LogCapture struct {
	buf       *bytes.Buffer
	prevOut   io.Writer
	prevFlags int
	prevPref  string
}

// CaptureLogs redirects the standard logger to a buffer.
func CaptureLogs() *LogCapture {
	buf := &bytes.Buffer{}
	capture := &LogCapture{
		buf:       buf,
		prevOut:   log.Writer(),
		prevFlags: log.Flags(),
		prevPref:  log.Prefix(),
	}
	log.SetOutput(capture.buf)
	log.SetFlags(0)
	log.SetPrefix("")
	return capture
}

// Stop restores logger output and returns captured logs.
func (l *LogCapture) Stop() string {
	log.SetOutput(l.prevOut)
	log.SetFlags(l.prevFlags)
	log.SetPrefix(l.prevPref)
	return l.buf.String()
}

// String returns captured logs without stopping capture.
func (l *LogCapture) String() string { return l.buf.String() }
