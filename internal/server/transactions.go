package server

import "github.com/luoyu/rpskill/internal/storage"

// Only actual workspace file transactions require recovery, never requests.
func (s *Server) recoveryPending() (bool, error) { return storage.Pending(s.runtime) }
