package auth

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func tokensFile(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func TestAuthenticate(t *testing.T) {
	set, err := Parse(strings.NewReader(tokensFile(
		"# comment",
		"hermes read "+Hash("secret-read"),
		"phone read,tasks:write "+Hash("secret-write"),
	)))
	if err != nil {
		t.Fatal(err)
	}
	if tok := set.Authenticate("Bearer secret-read"); tok == nil || tok.Name != "hermes" || tok.Has(ScopeTasksWrite) {
		t.Fatalf("read token: %#v", tok)
	}
	if tok := set.Authenticate("bearer secret-write"); tok == nil || !tok.Has(ScopeTasksWrite) {
		t.Fatalf("write token: %#v", tok)
	}
	for _, h := range []string{"", "Bearer", "Bearer ", "Basic secret-read", "Bearer wrong", "secret-read"} {
		if tok := set.Authenticate(h); tok != nil {
			t.Fatalf("%q authenticated as %s", h, tok.Name)
		}
	}
}

func TestParseRejectsBadFiles(t *testing.T) {
	for _, file := range []string{
		"",
		"only-two fields",
		"x read nothex",
		"x admin " + Hash("t"),
		tokensFile("x read "+Hash("a"), "x read "+Hash("b")),
	} {
		if _, err := Parse(strings.NewReader(file)); err == nil {
			t.Fatalf("accepted %q", file)
		}
	}
}

func TestNewTokenIsUnique(t *testing.T) {
	a, _ := NewToken()
	b, _ := NewToken()
	if a == b || len(a) < 40 {
		t.Fatalf("tokens %q %q", a, b)
	}
}

func TestFailureLimiter(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	l := NewFailureLimiter(10, time.Minute)
	l.Now = func() time.Time { return now }
	for i := 0; i < 10; i++ {
		if l.Blocked("1.2.3.4") {
			t.Fatalf("blocked after %d failures", i)
		}
		l.Fail("1.2.3.4")
	}
	if !l.Blocked("1.2.3.4") {
		t.Fatal("not blocked after 10 failures")
	}
	if l.Blocked("5.6.7.8") {
		t.Fatal("other address blocked")
	}
	now = now.Add(time.Minute)
	if l.Blocked("1.2.3.4") {
		t.Fatal("still blocked in the next window")
	}
}

func TestFailureLimiterIsBounded(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	l := NewFailureLimiter(1, time.Minute)
	l.Now = func() time.Time { return now }
	for i := 0; i < 3*maxEntries; i++ {
		l.Blocked(fmt.Sprint("ok-", i))
	}
	if len(l.entries) != 0 {
		t.Fatalf("Blocked recorded %d entries", len(l.entries))
	}
	l.Fail("first")
	for i := 0; i < maxEntries+100; i++ {
		now = now.Add(time.Millisecond)
		l.Fail(fmt.Sprint("bad-", i))
	}
	if len(l.entries) > maxEntries {
		t.Fatalf("%d entries, want at most %d", len(l.entries), maxEntries)
	}
	if l.Blocked("first") {
		t.Fatal("oldest entry was not evicted")
	}
	if !l.Blocked(fmt.Sprint("bad-", maxEntries+99)) {
		t.Fatal("newest entry was evicted")
	}
}
