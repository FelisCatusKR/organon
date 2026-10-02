package idem

import (
	"testing"
	"time"
)

func TestStore(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	s := New(2, time.Hour)
	s.Now = func() time.Time { return now }

	if o, _ := s.Begin("k1", "a"); o != Proceed {
		t.Fatalf("first Begin = %v", o)
	}
	if o, _ := s.Begin("k1", "a"); o != InProgress {
		t.Fatalf("in-flight Begin = %v", o)
	}
	s.Finish("k1", Response{Status: 201, Body: []byte("x")})
	if o, r := s.Begin("k1", "a"); o != Replay || r.Status != 201 {
		t.Fatalf("replay = %v %v", o, r)
	}
	if o, _ := s.Begin("k1", "b"); o != Mismatch {
		t.Fatalf("mismatch = %v", o)
	}

	s.Begin("k2", "a")
	s.Abort("k2")
	if o, _ := s.Begin("k2", "a"); o != Proceed {
		t.Fatalf("after abort = %v", o)
	}

	s.Begin("k3", "a") // evicts least recently used (k1 was used before k2)
	if o, _ := s.Begin("k1", "a"); o != Proceed {
		t.Fatalf("evicted key = %v", o)
	}

	now = now.Add(2 * time.Hour)
	if o, _ := s.Begin("k3", "a"); o != Proceed {
		t.Fatalf("expired key = %v", o)
	}
}
