package transport

import (
	"bytes"
	"errors"
	"io"
	"sync"

	"github.com/gorilla/websocket"
)

type wsStream struct {
	conn    *websocket.Conn
	r       io.Reader
	writeMu sync.Mutex
	write   bytes.Buffer
}

func newWSStream(conn *websocket.Conn) *wsStream {
	return &wsStream{conn: conn}
}

func (s *wsStream) Read(p []byte) (int, error) {
	for {
		if s.r == nil {
			msgType, r, err := s.conn.NextReader()
			if err != nil {
				return 0, err
			}
			if msgType != websocket.BinaryMessage {
				return 0, errors.New("transport: websocket non-binary message")
			}
			s.r = r
		}
		n, err := s.r.Read(p)
		if err == io.EOF {
			s.r = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (s *wsStream) Write(p []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.write.Write(p)
}

// Flush emits all writes since the previous flush as one WebSocket message.
// MTProto-over-WebSocket clients use the WebSocket message boundary as the
// transport-packet boundary, so allowing bufio.Writer to emit partial messages
// corrupts packets larger than its internal buffer.
func (s *wsStream) Flush() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.write.Len() == 0 {
		return nil
	}
	w, err := s.conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return err
	}
	_, err = s.write.WriteTo(w)
	closeErr := w.Close()
	if err != nil {
		return err
	}
	return closeErr
}
