package transport

import (
	"errors"
	"io"

	"github.com/gorilla/websocket"
)

type wsStream struct {
	conn *websocket.Conn
	r    io.Reader
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
	w, err := s.conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return 0, err
	}
	n, err := w.Write(p)
	closeErr := w.Close()
	if err != nil {
		return n, err
	}
	return n, closeErr
}
