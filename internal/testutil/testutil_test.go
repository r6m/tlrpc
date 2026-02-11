package testutil

import (
	"bytes"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMust(t *testing.T) {
	require.NotPanics(t, func() { Must(nil) })
	require.Panics(t, func() { Must(errors.New("boom")) })
}

func TestTempFile(t *testing.T) {
	file := TempFile()
	t.Cleanup(func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	})

	info, err := os.Stat(file.Name())
	require.NoError(t, err)
	require.False(t, info.IsDir())
}

func TestRandBytes(t *testing.T) {
	data := RandBytes(32)
	require.Len(t, data, 32)
	require.False(t, bytes.Equal(data, make([]byte, 32)))
}

func TestCaptureLogs(t *testing.T) {
	capture := CaptureLogs()
	log.Print("hello")
	output := capture.Stop()
	require.Contains(t, output, "hello")
}
