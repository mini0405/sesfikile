package stops

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("stop request not found")

// Store is a concurrency-safe, in-memory map of request_id -> Request,
// guarded by a single RWMutex — same pattern as
// telemetry.VehicleStateStore. In memory only; requests reset on server
// restart (see models.go's SCOPE HONESTY note).
type Store struct {
	mu       sync.RWMutex
	requests map[uuid.UUID]Request
}

func NewStore() *Store {
	return &Store{requests: make(map[uuid.UUID]Request)}
}

// Put inserts or overwrites a request record.
func (s *Store) Put(req Request) Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests[req.ID] = req
	return req
}

// Get returns one request by id.
func (s *Store) Get(id uuid.UUID) (Request, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.requests[id]
	return r, ok
}

// Acknowledge marks a request acknowledged. Returns ErrNotFound if the
// request doesn't exist. Acknowledging an already-acknowledged request is a
// no-op that returns the existing record (idempotent, not an error) — a
// driver double-tapping "picked up" shouldn't see a failure.
func (s *Store) Acknowledge(id uuid.UUID) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return Request{}, ErrNotFound
	}
	if req.Status == StatusAcknowledged {
		return req, nil
	}
	now := time.Now()
	req.Status = StatusAcknowledged
	req.AckedAt = &now
	s.requests[id] = req
	return req, nil
}
