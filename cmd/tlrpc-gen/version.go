package main

import (
	"fmt"
	"runtime"
)

const (
	version = "0.1.0"
)

var (
	commit  = "unknown"
	date    = "unknown"
)

// versionCommand displays version information
type versionCommand struct{}

func newVersionCommand() *versionCommand {
	return &versionCommand{}
}

func (v *versionCommand) run() error {
	fmt.Printf("tlrpc-gen version %s\n", version)
	fmt.Printf("  Commit: %s\n", commit)
	fmt.Printf("  Built: %s\n", date)
	fmt.Printf("  Go version: %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}