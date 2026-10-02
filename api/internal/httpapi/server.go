// Package httpapi is the public HTTP API (/api/v1). It authenticates and
// validates requests, forwards them to the engine, and returns the engine's
// results as the JSON types in package model. It contains no Org logic and no
// date arithmetic: everything about tasks is decided by the engine.
//
// The contract is api/openapi.yaml; behavior is specified in
// openspec/specs/.
package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/FelisCatusKR/organon/api/internal/auth"
	"github.com/FelisCatusKR/organon/api/internal/idem"
	"github.com/FelisCatusKR/organon/api/internal/model"
	"github.com/FelisCatusKR/organon/api/internal/rpc"
)

// Engine is the part of rpc.Client the server uses (a fake in tests).
type Engine interface {
	Call(ctx context.Context, method string, params, out any) error
}

// Server holds the API's dependencies.
type Server struct {
	Engine         Engine
	Tokens         *auth.Set
	Limiter        *auth.FailureLimiter
	Idempotency    *idem.Store
	ClientIPHeader string // e.g. "CF-Connecting-IP" behind cloudflared; empty: use the TCP peer
	Version        string
	Logger         *slog.Logger
}

const maxBodyBytes = 1 << 20

// Handler returns the HTTP handler for the whole API.
func (s *Server) Handler() http.Handler {
	read, write := auth.ScopeRead, auth.ScopeTasksWrite
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.Handle("GET /api/v1/meta", s.authorize(read, s.meta))
	mux.Handle("GET /api/v1/agenda", s.authorize(read, s.agenda))
	mux.Handle("GET /api/v1/tasks/today", s.authorize(read, s.taskList("tasks.today", true)))
	mux.Handle("GET /api/v1/tasks/overdue", s.authorize(read, s.taskList("tasks.overdue", true)))
	mux.Handle("GET /api/v1/tasks/waiting", s.authorize(read, s.taskList("tasks.waiting", false)))
	mux.Handle("GET /api/v1/tasks/completed", s.authorize(read, s.taskList("tasks.completed", true)))
	mux.Handle("GET /api/v1/tasks/{id}", s.authorize(read, s.getTask))
	mux.Handle("POST /api/v1/tasks", s.authorize(write, s.createTask))
	mux.Handle("POST /api/v1/tasks/{id}/{action}", s.authorize(write, s.transition))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, model.ProblemCodeNotFound, "no such endpoint", nil)
	})
	return s.logRequests(mux)
}

// ---- middleware -------------------------------------------------------------

type tokenKey struct{}

func (s *Server) authorize(scope string, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr := s.clientAddr(r)
		if s.Limiter.Blocked(addr) {
			writeProblem(w, http.StatusTooManyRequests, model.ProblemCodeRateLimited,
				"too many failed authentication attempts; try again later", nil)
			return
		}
		tok := s.Tokens.Authenticate(r.Header.Get("Authorization"))
		if tok == nil {
			s.Limiter.Fail(addr)
			w.Header().Set("WWW-Authenticate", `Bearer realm="organon"`)
			writeProblem(w, http.StatusUnauthorized, model.ProblemCodeUnauthorized, "missing or invalid bearer token", nil)
			return
		}
		if !tok.Has(scope) {
			writeProblem(w, http.StatusForbidden, model.ProblemCodeForbidden, "token lacks scope "+scope, nil)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), tokenKey{}, tok)))
	})
}

// clientAddr is the address failed authentications are counted against. The
// configured header is trusted as set by the proxy in front of the API: for a
// list (X-Forwarded-For) the last entry is the one that proxy appended. A value
// that is not an IP address is ignored.
func (s *Server) clientAddr(r *http.Request) string {
	if s.ClientIPHeader != "" {
		if v := r.Header.Get(s.ClientIPHeader); v != "" {
			last := v[strings.LastIndex(v, ",")+1:]
			if addr, err := netip.ParseAddr(strings.TrimSpace(last)); err == nil {
				return addr.Unmap().String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if s.Logger != nil {
			s.Logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
				"ms", time.Since(start).Milliseconds())
		}
	})
}

// ---- responses ----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) []byte {
	body, err := json.Marshal(v)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, model.ProblemCodeInternal, "could not encode the response", nil)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
	return body
}

// writeProblem writes an RFC 9457 problem document. ext adds extension members
// (for example actual_state on a conflict).
func writeProblem(w http.ResponseWriter, status int, code model.ProblemCode, detail string, ext map[string]any) {
	p := model.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Code: code, Detail: detail}
	for k, v := range ext {
		switch s, _ := v.(string); k {
		case "actual_state":
			state := model.State(s)
			p.ActualState = &state
		case "actual_version":
			p.ActualVersion = &s
		default:
			if p.AdditionalProperties == nil {
				p.AdditionalProperties = map[string]any{}
			}
			p.AdditionalProperties[k] = v
		}
	}
	body, _ := json.Marshal(p)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	w.Write(body)
}

// writeEngineError maps an engine error to an HTTP problem. Engine-side
// failures are logged; their messages (socket paths, Lisp errors) are not sent
// to the client.
func (s *Server) writeEngineError(w http.ResponseWriter, r *http.Request, err error) {
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) {
		s.logEngineError(r, "internal", err)
		writeProblem(w, http.StatusInternalServerError, model.ProblemCodeInternal, "internal error", nil)
		return
	}
	switch rpcErr.Code {
	case rpc.CodeInvalid:
		writeProblem(w, http.StatusUnprocessableEntity, model.ProblemCodeInvalid, rpcErr.Message, rpcErr.Data)
	case rpc.CodeNotFound:
		writeProblem(w, http.StatusNotFound, model.ProblemCodeNotFound, rpcErr.Message, rpcErr.Data)
	case rpc.CodeConflict:
		writeProblem(w, http.StatusConflict, model.ProblemCodeConflict, rpcErr.Message, rpcErr.Data)
	case rpc.CodeUnavailable:
		s.logEngineError(r, rpcErr.Code, err)
		writeProblem(w, http.StatusServiceUnavailable, model.ProblemCodeEngineUnavailable, "the engine is unavailable", nil)
	default:
		s.logEngineError(r, rpcErr.Code, err)
		writeProblem(w, http.StatusInternalServerError, model.ProblemCodeInternal, "internal error", nil)
	}
}

func (s *Server) logEngineError(r *http.Request, code string, err error) {
	if s.Logger != nil {
		s.Logger.Error("engine error", "method", r.Method, "path", r.URL.Path, "code", code, "error", err)
	}
}

func invalid(w http.ResponseWriter, detail string) {
	writeProblem(w, http.StatusUnprocessableEntity, model.ProblemCodeInvalid, detail, nil)
}

// ---- input validation -----------------------------------------------------------

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// pathID returns the {id} path value if it is a lowercase UUID.
func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !uuidRE.MatchString(id) {
		invalid(w, "id must be a lowercase UUID")
		return "", false
	}
	return id, true
}

// queryDate returns the optional ?date= parameter if it is a calendar date.
func queryDate(w http.ResponseWriter, r *http.Request) (string, bool) {
	date := r.URL.Query().Get("date")
	if date == "" {
		return "", true
	}
	if _, err := time.Parse(time.DateOnly, date); err != nil || len(date) != 10 {
		invalid(w, "date must be a calendar date YYYY-MM-DD")
		return "", false
	}
	return date, true
}

// decodeBody strictly decodes a JSON object body into v.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		invalid(w, "request body too large or unreadable")
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		invalid(w, "invalid JSON body: "+err.Error())
		return nil, false
	}
	if dec.More() {
		invalid(w, "body must contain a single JSON object")
		return nil, false
	}
	return raw, true
}

// ---- handlers --------------------------------------------------------------------

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.Engine.Call(ctx, "ping", nil, nil); err != nil {
		s.writeEngineError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	var meta model.Meta
	if err := s.Engine.Call(r.Context(), "meta", nil, &meta); err != nil {
		s.writeEngineError(w, r, err)
		return
	}
	meta.Version = s.Version
	writeJSON(w, http.StatusOK, meta)
}

func dateParams(date string) map[string]any {
	if date == "" {
		return map[string]any{}
	}
	return map[string]any{"date": date}
}

func (s *Server) agenda(w http.ResponseWriter, r *http.Request) {
	date, ok := queryDate(w, r)
	if !ok {
		return
	}
	var items []model.AgendaEntry
	if err := s.Engine.Call(r.Context(), "agenda.day", dateParams(date), &items); err != nil {
		s.writeEngineError(w, r, err)
		return
	}
	for i := range items {
		if items[i].Task != nil {
			items[i].Task.Normalize()
		}
	}
	if items == nil {
		items = []model.AgendaEntry{}
	}
	writeJSON(w, http.StatusOK, model.AgendaList{Items: items})
}

func (s *Server) taskList(method string, takesDate bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params := map[string]any{}
		if takesDate {
			date, ok := queryDate(w, r)
			if !ok {
				return
			}
			params = dateParams(date)
		}
		var items []model.Task
		if err := s.Engine.Call(r.Context(), method, params, &items); err != nil {
			s.writeEngineError(w, r, err)
			return
		}
		for i := range items {
			items[i].Normalize()
		}
		if items == nil {
			items = []model.Task{}
		}
		writeJSON(w, http.StatusOK, model.TaskList{Items: items})
	}
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var task model.Task
	if err := s.Engine.Call(r.Context(), "task.get", map[string]any{"id": id}, &task); err != nil {
		s.writeEngineError(w, r, err)
		return
	}
	task.Normalize()
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var req model.CreateTask
	if _, ok := decodeBody(w, r, &req); !ok {
		return
	}

	key := r.Header.Get("Idempotency-Key")
	storeKey := ""
	if key != "" {
		if len(key) > 200 {
			invalid(w, "Idempotency-Key must be at most 200 characters")
			return
		}
		canonical, _ := json.Marshal(req)
		sum := sha256.Sum256(canonical)
		tok := r.Context().Value(tokenKey{}).(*auth.Token)
		storeKey = tok.Name + "\x00" + key
		switch outcome, stored := s.Idempotency.Begin(storeKey, hex.EncodeToString(sum[:])); outcome {
		case idem.Replay:
			w.Header().Set("Content-Type", "application/json")
			if stored.Location != "" {
				w.Header().Set("Location", stored.Location)
			}
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(stored.Status)
			w.Write(stored.Body)
			return
		case idem.Mismatch:
			invalid(w, "Idempotency-Key was already used with a different request body")
			return
		case idem.InProgress:
			writeProblem(w, http.StatusConflict, model.ProblemCodeConflict, "a request with this Idempotency-Key is in progress", nil)
			return
		}
	}

	var task model.Task
	if err := s.Engine.Call(r.Context(), "task.create", req, &task); err != nil {
		if storeKey != "" {
			s.Idempotency.Abort(storeKey)
		}
		s.writeEngineError(w, r, err)
		return
	}
	task.Normalize()
	location := "/api/v1/tasks/" + task.ID
	w.Header().Set("Location", location)
	body := writeJSON(w, http.StatusCreated, task)
	if storeKey != "" {
		s.Idempotency.Finish(storeKey, idem.Response{Status: http.StatusCreated, Body: body, Location: location})
	}
}

var actions = map[string]bool{"start": true, "wait": true, "complete": true, "skip": true, "cancel": true}

func (s *Server) transition(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	action := r.PathValue("action")
	if !actions[action] {
		writeProblem(w, http.StatusNotFound, model.ProblemCodeNotFound, "unknown action "+action, nil)
		return
	}
	var req model.Transition
	if _, ok := decodeBody(w, r, &req); !ok {
		return
	}
	if req.ExpectedState == "" {
		invalid(w, "expected_state is required")
		return
	}
	if !req.ExpectedState.Valid() {
		invalid(w, "expected_state is not a task state")
		return
	}
	params := map[string]any{"id": id, "action": action, "expected_state": req.ExpectedState}
	if req.ExpectedVersion != nil {
		params["expected_version"] = *req.ExpectedVersion
	}
	var result model.TransitionResult
	if err := s.Engine.Call(r.Context(), "task.transition", params, &result); err != nil {
		s.writeEngineError(w, r, err)
		return
	}
	result.Task.Normalize()
	if result.Warnings == nil {
		result.Warnings = []model.TransitionResultWarnings{}
	}
	writeJSON(w, http.StatusOK, result)
}
