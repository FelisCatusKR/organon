// Command organon is the single binary shipped in the Organon image.
//
//	organon serve                         run the HTTP API
//	organon healthcheck                   exit 0 if the API answers /healthz with 200
//	organon rpc-ping                      exit 0 if the engine socket answers ping
//	organon init [--calendar-tz ZONE]     create a new data directory
//	organon token new --name N --scopes S print a new token and its tokens-file line
//	organon token hash                    print the SHA-256 of a token read from stdin
//	organon version
//
// Configuration comes from the environment (see docs/deployment.md):
//
//	ORGANON_LISTEN            address for serve (default 127.0.0.1:8080)
//	ORGANON_RUN_DIR           directory holding rpc.sock (default /run/organon)
//	ORGANON_DATA_DIR          data directory for init (default /data)
//	ORGANON_TOKENS_FILE       tokens file for serve (required)
//	ORGANON_ENGINE_TIMEOUT    per-request engine timeout (default 10s)
//	ORGANON_CLIENT_IP_HEADER  header holding the client address behind a proxy
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/FelisCatusKR/organon/api/internal/auth"
	"github.com/FelisCatusKR/organon/api/internal/cli"
	"github.com/FelisCatusKR/organon/api/internal/httpapi"
	"github.com/FelisCatusKR/organon/api/internal/idem"
	"github.com/FelisCatusKR/organon/api/internal/instance"
	"github.com/FelisCatusKR/organon/api/internal/rpc"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	// Client commands talk to the HTTP API (package cli).
	if cli.Commands[os.Args[1]] {
		os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
	}
	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "serve":
		err = serve()
	case "healthcheck":
		err = healthcheck()
	case "rpc-ping":
		err = rpcPing()
	case "init":
		err = initInstance(args)
	case "token":
		err = token(args)
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "organon:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: organon <command>

server commands:
  serve                           run the HTTP API
  healthcheck                     check the API's /healthz
  rpc-ping                        check the engine socket
  init [--calendar-tz ZONE]       create a new data directory
  token new --name N --scopes S   create a token (scopes: read, tasks:write)
  token hash                      hash a token read from stdin
  version

`+cli.Usage+"\n")
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func engineClient() (*rpc.Client, error) {
	timeout, err := time.ParseDuration(env("ORGANON_ENGINE_TIMEOUT", "10s"))
	if err != nil || timeout <= 0 {
		return nil, fmt.Errorf("invalid ORGANON_ENGINE_TIMEOUT: %q", os.Getenv("ORGANON_ENGINE_TIMEOUT"))
	}
	return &rpc.Client{SocketPath: filepath.Join(env("ORGANON_RUN_DIR", "/run/organon"), "rpc.sock"), Timeout: timeout}, nil
}

func serve() error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client, err := engineClient()
	if err != nil {
		return err
	}
	tokensFile := os.Getenv("ORGANON_TOKENS_FILE")
	if tokensFile == "" {
		return errors.New("ORGANON_TOKENS_FILE is required (create entries with `organon token new`)")
	}
	tokens, err := auth.Load(tokensFile)
	if err != nil {
		return fmt.Errorf("loading tokens: %w", err)
	}
	api := &httpapi.Server{
		Engine:         client,
		Tokens:         tokens,
		Limiter:        auth.NewFailureLimiter(10, time.Minute),
		Idempotency:    idem.New(10000, 24*time.Hour),
		ClientIPHeader: os.Getenv("ORGANON_CLIENT_IP_HEADER"),
		Version:        version,
		Logger:         logger,
	}
	addr := env("ORGANON_LISTEN", "127.0.0.1:8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      client.Timeout + 10*time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logger.Info("listening", "addr", addr, "version", version)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}

func healthcheck() error {
	addr := env("ORGANON_LISTEN", "127.0.0.1:8080")
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %s", resp.Status)
	}
	return nil
}

func rpcPing() error {
	client, err := engineClient()
	if err != nil {
		return err
	}
	client.Timeout = 5 * time.Second
	return client.Call(context.Background(), "ping", nil, nil)
}

func initInstance(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	dataDir := fs.String("data-dir", env("ORGANON_DATA_DIR", "/data"), "data directory to initialize")
	zone := fs.String("calendar-tz", "", "IANA time zone the Org files are written in, e.g. Asia/Seoul")
	limit := fs.Int("doing-limit", 3, "warn when more tasks than this are DOING")
	fs.Parse(args)
	if *zone == "" {
		if detected := instance.DetectZone(); detected != "" {
			return fmt.Errorf("--calendar-tz is required; this host uses %s, so run:\n  organon init --calendar-tz %s", detected, detected)
		}
		return errors.New("--calendar-tz is required (an IANA zone such as Asia/Seoul)")
	}
	if err := instance.Init(*dataDir, instance.Config{CalendarTZ: *zone, DoingLimit: *limit}); err != nil {
		return err
	}
	fmt.Printf("initialized %s (calendar_tz %s)\n", *dataDir, *zone)
	return nil
}

func token(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: organon token new --name NAME --scopes read[,tasks:write] | organon token hash")
	}
	switch args[0] {
	case "new":
		fs := flag.NewFlagSet("token new", flag.ExitOnError)
		name := fs.String("name", "", "client name, e.g. hermes")
		scopes := fs.String("scopes", auth.ScopeRead, "comma-separated scopes: read, tasks:write")
		fs.Parse(args[1:])
		if *name == "" || strings.ContainsAny(*name, " \t\n") {
			return errors.New("--name is required and must not contain spaces")
		}
		if err := auth.ValidScopes(*scopes); err != nil {
			return err
		}
		tok, err := auth.NewToken()
		if err != nil {
			return err
		}
		fmt.Printf("token (shown once, give it to the client):\n  %s\n", tok)
		fmt.Printf("tokens file line:\n  %s %s %s\n", *name, *scopes, auth.Hash(tok))
		return nil
	case "hash":
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return errors.New("reading token from stdin: " + err.Error())
		}
		fmt.Println(auth.Hash(strings.TrimSpace(line)))
		return nil
	}
	return fmt.Errorf("unknown token command %q", args[0])
}
