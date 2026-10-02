// Package auth implements bearer-token authentication with scopes and a
// per-address limit on failed attempts (spec: api-access).
//
// Tokens are never stored: the tokens file holds SHA-256 hashes, one per line:
//
//	# name   scopes             sha256(token), hex
//	hermes   read               5e8848...
//	phone    read,tasks:write   0b14d5...
//
// Generate an entry with `organon token new --name NAME --scopes SCOPES`.
package auth

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// Scopes understood by the API.
const (
	ScopeRead       = "read"
	ScopeTasksWrite = "tasks:write"
)

var knownScopes = map[string]bool{ScopeRead: true, ScopeTasksWrite: true}

// Token is one configured client.
type Token struct {
	Name   string
	Scopes map[string]bool
	hash   [sha256.Size]byte
}

// Has reports whether the token carries scope.
func (t *Token) Has(scope string) bool { return t.Scopes[scope] }

// Set is the configured tokens.
type Set struct{ tokens []*Token }

// Load reads a tokens file.
func Load(path string) (*Set, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads tokens in the file format described in the package comment.
func Parse(r io.Reader) (*Set, error) {
	set := &Set{}
	names := map[string]bool{}
	scanner := bufio.NewScanner(r)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("tokens line %d: want \"name scopes sha256hex\"", n)
		}
		name, scopeList, hexHash := fields[0], fields[1], fields[2]
		if names[name] {
			return nil, fmt.Errorf("tokens line %d: duplicate name %q", n, name)
		}
		names[name] = true
		tok := &Token{Name: name, Scopes: map[string]bool{}}
		for _, s := range strings.Split(scopeList, ",") {
			if !knownScopes[s] {
				return nil, fmt.Errorf("tokens line %d: unknown scope %q", n, s)
			}
			tok.Scopes[s] = true
		}
		raw, err := hex.DecodeString(hexHash)
		if err != nil || len(raw) != sha256.Size {
			return nil, fmt.Errorf("tokens line %d: hash must be 64 hex characters", n)
		}
		copy(tok.hash[:], raw)
		set.tokens = append(set.tokens, tok)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(set.tokens) == 0 {
		return nil, fmt.Errorf("no tokens configured")
	}
	return set, nil
}

// Authenticate returns the token matching the Authorization header value,
// or nil. Every configured hash is compared, in constant time.
func (s *Set) Authenticate(header string) *Token {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return nil
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(header[len(prefix):])))
	var found *Token
	for _, t := range s.tokens {
		if subtle.ConstantTimeCompare(sum[:], t.hash[:]) == 1 {
			found = t
		}
	}
	return found
}

// Hash returns the hex SHA-256 of a token, as written in the tokens file.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken returns a random 256-bit token.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "org_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// ValidScopes checks a comma-separated scope list.
func ValidScopes(list string) error {
	for _, s := range strings.Split(list, ",") {
		if !knownScopes[s] {
			return fmt.Errorf("unknown scope %q (known: read, tasks:write)", s)
		}
	}
	return nil
}
