package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FelisCatusKR/organon/api/internal/auth"
)

// The tests run the command as a subprocess of the test binary, so that
// main's os.Exit and its use of stdin/stdout are exercised as they are.
func TestMain(m *testing.M) {
	if os.Getenv("ORGANON_TEST_RUN_MAIN") == "1" {
		os.Args = append([]string{"organon"}, strings.Fields(os.Getenv("ORGANON_TEST_ARGS"))...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// run executes `organon args...` and returns stdout, stderr and the exit code.
func run(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "ORGANON_TEST_RUN_MAIN=1", "ORGANON_TEST_ARGS="+strings.Join(args, " "))
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

func TestTokenNewPrintsTokenAndMatchingLine(t *testing.T) {
	out, errOut, code := run(t, "", "token", "new", "--name", "hermes", "--scopes", "read,tasks:write")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("output:\n%s", out)
	}
	tok := strings.TrimSpace(lines[1])
	want := "hermes read,tasks:write " + auth.Hash(tok)
	if strings.TrimSpace(lines[3]) != want {
		t.Fatalf("tokens file line %q, want %q", lines[3], want)
	}
	set, err := auth.Parse(strings.NewReader(want + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Authenticate("Bearer " + tok); got == nil || got.Name != "hermes" {
		t.Fatalf("printed token does not authenticate against its line: %+v", got)
	}
}

func TestTokenNewRejectsBadInput(t *testing.T) {
	for _, args := range [][]string{
		{"token", "new"},
		{"token", "new", "--name", "x", "--scopes", "admin"},
		{"token", "frobnicate"},
	} {
		if _, _, code := run(t, "", args...); code != 1 {
			t.Errorf("%v: exit %d, want 1", args, code)
		}
	}
}

func TestTokenHashReadsStdin(t *testing.T) {
	out, errOut, code := run(t, "org_secret\n", "token", "hash")
	if code != 0 || strings.TrimSpace(out) != auth.Hash("org_secret") {
		t.Fatalf("exit %d, out %q, err %q", code, out, errOut)
	}
}

func TestInit(t *testing.T) {
	dir := t.TempDir()
	if _, errOut, code := run(t, "", "init", "--data-dir", dir); code != 1 || !strings.Contains(errOut, "--calendar-tz is required") {
		t.Fatalf("without zone: exit %d, %s", code, errOut)
	}
	if _, errOut, code := run(t, "", "init", "--data-dir", dir, "--calendar-tz", "localtime"); code != 1 {
		t.Fatalf("host zone accepted: exit %d, %s", code, errOut)
	}
	if _, errOut, code := run(t, "", "init", "--data-dir", dir, "--calendar-tz", "Asia/Seoul"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "organon.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		CalendarTZ string `json:"calendar_tz"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.CalendarTZ != "Asia/Seoul" {
		t.Fatalf("organon.json: %s", raw)
	}
	if _, _, code := run(t, "", "init", "--data-dir", dir, "--calendar-tz", "Asia/Seoul"); code != 1 {
		t.Fatalf("re-init of an existing instance: exit %d", code)
	}
}

func TestUsageAndVersion(t *testing.T) {
	if out, _, code := run(t, "", "version"); code != 0 || strings.TrimSpace(out) != "dev" {
		t.Fatalf("version: exit %d, %q", code, out)
	}
	if _, errOut, code := run(t, "", "frobnicate"); code != 2 || !strings.Contains(errOut, "usage: organon") {
		t.Fatalf("unknown command: exit %d, %q", code, errOut)
	}
}

func TestServeRequiresTokensFile(t *testing.T) {
	t.Setenv("ORGANON_TOKENS_FILE", "")
	if _, errOut, code := run(t, "", "serve"); code != 1 || !strings.Contains(errOut, "ORGANON_TOKENS_FILE") {
		t.Fatalf("exit %d, %q", code, errOut)
	}
}
