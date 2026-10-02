// Package client is a small HTTP client for the Organon API (/api/v1). It is
// what the CLI uses; any other Go program can use it the same way. Methods
// return the raw response body next to the decoded value, so callers can
// print the API's JSON unchanged.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FelisCatusKR/organon/api/internal/model"
)

// Client talks to one Organon instance.
type Client struct {
	BaseURL string // e.g. https://organon.example.org (without /api/v1)
	Token   string
	HTTP    *http.Client
}

// New returns a client with a 30-second timeout.
func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// APIError is an error response (an RFC 9457 problem document).
type APIError struct {
	Status int
	Code   string
	Detail string
	Body   []byte // the problem document as received
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// TransportError means the request may or may not have reached the engine:
// the connection failed or timed out before a response arrived.
type TransportError struct{ Err error }

func (e *TransportError) Error() string { return e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }

// OutcomeUnknown reports whether err leaves it open whether a change was
// applied: no response at all, or the engine did not answer in time.
func OutcomeUnknown(err error) bool {
	var te *TransportError
	var ae *APIError
	return errors.As(err, &te) || (errors.As(err, &ae) && ae.Status == http.StatusServiceUnavailable)
}

// Do sends a request and decodes a successful JSON response into out (if
// non-nil). It returns the raw response body in every case it has one.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) ([]byte, error) {
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(b)
	}
	target := c.BaseURL + "/api/v1" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	if resp.StatusCode >= 400 {
		apiErr := &APIError{Status: resp.StatusCode, Body: raw}
		var problem struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal(raw, &problem) == nil {
			apiErr.Code, apiErr.Detail = problem.Code, problem.Detail
		}
		return raw, apiErr
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return raw, fmt.Errorf("unexpected response: %w", err)
		}
	}
	return raw, nil
}

func dateQuery(date string) url.Values {
	if date == "" {
		return nil
	}
	return url.Values{"date": {date}}
}

// TaskView fetches one of the agenda-based lists: today, overdue, completed
// (date may be empty) or waiting.
func (c *Client) TaskView(ctx context.Context, view, date string) (model.TaskList, []byte, error) {
	var list model.TaskList
	raw, err := c.Do(ctx, http.MethodGet, "/tasks/"+view, dateQuery(date), nil, &list)
	return list, raw, err
}

// ListTasks lists tasks by states (empty: open ones), project and tag.
func (c *Client) ListTasks(ctx context.Context, states []string, project, tag string) (model.TaskList, []byte, error) {
	q := url.Values{}
	if len(states) > 0 {
		q.Set("state", strings.Join(states, ","))
	}
	if project != "" {
		q.Set("project", project)
	}
	if tag != "" {
		q.Set("tag", tag)
	}
	var list model.TaskList
	raw, err := c.Do(ctx, http.MethodGet, "/tasks", q, nil, &list)
	return list, raw, err
}

// GetTask reads one task.
func (c *Client) GetTask(ctx context.Context, id string) (model.Task, []byte, error) {
	var task model.Task
	raw, err := c.Do(ctx, http.MethodGet, "/tasks/"+url.PathEscape(id), nil, nil, &task)
	return task, raw, err
}

// CreateTask creates a task.
func (c *Client) CreateTask(ctx context.Context, req model.CreateTask) (model.Task, []byte, error) {
	var task model.Task
	raw, err := c.Do(ctx, http.MethodPost, "/tasks", nil, req, &task)
	return task, raw, err
}

// UpdateTask sends a PATCH. fields maps field names to new values; a nil
// value clears the field. expectedVersion is added by this method.
func (c *Client) UpdateTask(ctx context.Context, id, expectedVersion string, fields map[string]any) (model.Task, []byte, error) {
	body := map[string]any{"expected_version": expectedVersion}
	for k, v := range fields {
		body[k] = v
	}
	var task model.Task
	raw, err := c.Do(ctx, http.MethodPatch, "/tasks/"+url.PathEscape(id), nil, body, &task)
	return task, raw, err
}

// Transition changes a task's state.
func (c *Client) Transition(ctx context.Context, id, action, expectedState, expectedVersion string) (model.TransitionResult, []byte, error) {
	body := map[string]any{"expected_state": expectedState}
	if expectedVersion != "" {
		body["expected_version"] = expectedVersion
	}
	var result model.TransitionResult
	raw, err := c.Do(ctx, http.MethodPost, "/tasks/"+url.PathEscape(id)+"/"+url.PathEscape(action), nil, body, &result)
	return result, raw, err
}

// ListProjects lists projects.
func (c *Client) ListProjects(ctx context.Context) (model.ProjectList, []byte, error) {
	var list model.ProjectList
	raw, err := c.Do(ctx, http.MethodGet, "/projects", nil, nil, &list)
	return list, raw, err
}

// CreateProject creates a project.
func (c *Client) CreateProject(ctx context.Context, req model.CreateProject) (model.Project, []byte, error) {
	var project model.Project
	raw, err := c.Do(ctx, http.MethodPost, "/projects", nil, req, &project)
	return project, raw, err
}
