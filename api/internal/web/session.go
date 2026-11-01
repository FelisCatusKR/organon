package web

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"

	"github.com/FelisCatusKR/organon/api/internal/auth"
)

const (
	sessionLifetime = 30 * 24 * time.Hour
	maxSessions     = 1000
)

// sessions is the in-memory session store. It maps the SHA-256 of a session ID
// to its token, so that a memory dump holds no usable cookie. It never holds the
// raw API token. A restart empties it, which is also how a revoked token loses
// its sessions: the tokens file is only read at startup.
type sessions struct {
	now func() time.Time

	mu      sync.Mutex
	entries map[[sha256.Size]byte]session
}

type session struct {
	token   *auth.Token
	expires time.Time
}

func newSessions(now func() time.Time) *sessions {
	return &sessions{now: now, entries: map[[sha256.Size]byte]session{}}
}

// create starts a session and returns its ID, 256 random bits.
func (s *sessions) create(tok *auth.Token) (id string, expires time.Time, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	id = base64.RawURLEncoding.EncodeToString(raw)
	expires = s.now().Add(sessionLifetime)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= maxSessions {
		s.evict()
	}
	s.entries[sha256.Sum256([]byte(id))] = session{token: tok, expires: expires}
	return id, expires, nil
}

// get returns the session's token, or nil if the session is unknown or expired.
func (s *sessions) get(id string) *auth.Token {
	key := sha256.Sum256([]byte(id))
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return nil
	}
	if !s.now().Before(e.expires) {
		delete(s.entries, key)
		return nil
	}
	return e.token
}

// remove ends a session.
func (s *sessions) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, sha256.Sum256([]byte(id)))
}

// evict makes room: expired sessions go first, otherwise the one closest to expiry.
func (s *sessions) evict() {
	now := s.now()
	var oldest [sha256.Size]byte
	var oldestAt time.Time
	for key, e := range s.entries {
		if !now.Before(e.expires) {
			delete(s.entries, key)
		} else if oldestAt.IsZero() || e.expires.Before(oldestAt) {
			oldest, oldestAt = key, e.expires
		}
	}
	if len(s.entries) >= maxSessions {
		delete(s.entries, oldest)
	}
}
