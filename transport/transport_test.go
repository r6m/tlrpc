package transport

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	payloads := [][]byte{
		{},
		{0x01},
		{0x01, 0x02, 0x03},
		{0x01, 0x02, 0x03, 0x04},
		bytes.Repeat([]byte{0xAB}, 5),
		bytes.Repeat([]byte{0xCD}, 7),
		bytes.Repeat([]byte{0xEF}, 8),
		bytes.Repeat([]byte{0x11}, 9),
		bytes.Repeat([]byte{0x22}, 255),
	}

	for i, payload := range payloads {
		buf := &bytes.Buffer{}
		if err := writeFrame(buf, payload); err != nil {
			t.Fatalf("write frame %d: %v", i, err)
		}
		got, err := readFrame(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("read frame %d: %v", i, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("payload mismatch %d", i)
		}
	}
}
