// Package idem remembers responses to POST requests that carried an
// Idempotency-Key, so that a retried request returns the original response
// instead of creating a second task (spec: task-lifecycle, Idempotent creation).
//
// Entries live in memory only: after an API restart a key is unknown again.
package idem

import (
	"container/list"
	"sync"
	"time"
)

// Response is a stored HTTP response.
type Response struct {
	Status   int
	Body     []byte
	Location string
}

// Outcome of Begin.
type Outcome int

const (
	Proceed    Outcome = iota // first use of the key: run the request, then Finish or Abort
	Replay                    // same key and payload: send the stored response
	Mismatch                  // same key, different payload
	InProgress                // same key still being processed
)

type entry struct {
	key         string
	fingerprint string
	resp        *Response
	created     time.Time
}

// Store is a bounded LRU of idempotency keys.
type Store struct {
	Capacity int
	TTL      time.Duration
	Now      func() time.Time

	mu    sync.Mutex
	order *list.List // front = most recently used
	items map[string]*list.Element
}

// New creates a store holding up to capacity keys for ttl.
func New(capacity int, ttl time.Duration) *Store {
	return &Store{Capacity: capacity, TTL: ttl, Now: time.Now, order: list.New(), items: map[string]*list.Element{}}
}

// Begin claims key for a request whose payload hashes to fingerprint.
func (s *Store) Begin(key, fingerprint string) (Outcome, *Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.items[key]; ok {
		e := el.Value.(*entry)
		if s.Now().Sub(e.created) < s.TTL {
			s.order.MoveToFront(el)
			switch {
			case e.fingerprint != fingerprint:
				return Mismatch, nil
			case e.resp == nil:
				return InProgress, nil
			default:
				return Replay, e.resp
			}
		}
		s.order.Remove(el)
		delete(s.items, key)
	}
	s.items[key] = s.order.PushFront(&entry{key: key, fingerprint: fingerprint, created: s.Now()})
	for s.order.Len() > s.Capacity {
		oldest := s.order.Back()
		s.order.Remove(oldest)
		delete(s.items, oldest.Value.(*entry).key)
	}
	return Proceed, nil
}

// Finish stores the response for a key claimed with Begin.
func (s *Store) Finish(key string, resp Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.items[key]; ok {
		el.Value.(*entry).resp = &resp
	}
}

// Abort releases a claimed key without storing a response, so that a retry
// runs again (used when the request failed in a way worth retrying).
func (s *Store) Abort(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.items[key]; ok {
		s.order.Remove(el)
		delete(s.items, key)
	}
}
