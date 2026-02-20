//go:build !gotd_integ

package tlrpc

import "github.com/r6m/tlrpc/session"

func (s *Server) maybeForceBadServerSalt(_ *session.Session, _ int64, _ int32) (int64, bool) {
	return 0, false
}
