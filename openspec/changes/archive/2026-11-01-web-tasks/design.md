## Context

The decisions behind this change were made with the maintainer and are recorded on issue #25. This document covers
how they map onto the code. The API process has no data mount and reads its tokens file once at startup.

## Goals / Non-Goals

**Goals:** use from a phone browser; no change to the HTTP API or to deployments that leave the UI off; plain Go
that uses only the standard library.

**Non-Goals:** notes (`web-notes`), editing tasks, managing projects, offline use, multiple user accounts.

## Decisions

### D1: The UI calls the API in process

Pages get their data by sending requests through `httpapi.Server.InProcess(token)`, an `http.RoundTripper` that runs
the API's own handlers without a network hop. `internal/client` works on top of it unchanged. Validation, scope checks
and error mapping therefore have one implementation, and the UI cannot do anything the token could not do through
the API.

`authorize` accepts the identity from a private context key. Nothing that arrives over HTTP can set it. Such calls skip
the failure limiter because the session was already authenticated.

*Alternative:* a shared service layer, which is a large refactor of working handlers. Calling back over TCP needs a
listen address and the raw token, which we do not want to keep.

### D2: Sessions live in memory and keep no token

A session is a 256-bit random ID. The store maps its SHA-256 to the `*auth.Token` and an expiry 30 days after sign-in.
The raw token is dropped after the login request. A restart ends all sessions, and revoking a token requires a restart
because the tokens file is read once, so a revoked token can never outlive its sessions. At most 1000 sessions are kept;
the one closest to expiry goes first.

*Alternatives:* a cookie signed with the token hash would make a leaked hash enough to forge sessions, which it is
not today. A cookie signed with a separate key needs another secret and its own revocation.

### D3: Mounting

`Handler()` is unchanged while `Server.UI` is nil. Otherwise `/api/`, `/livez`, `/readyz` and `/healthz` go to the API,
and everything else to the UI. `/api/v1` never reads cookies, so CSRF cannot reach it.

### D4: Browser defenses

- CSRF: `SameSite=Strict` plus the standard library's `http.CrossOriginProtection`, which fails closed.
- CSP `default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors
  'none'`. The UI ships no JavaScript at all, so this CSP allows no script.
- `Referrer-Policy: no-referrer`. `Cache-Control: no-store` and `nosniff` come from the API's existing middleware.
- `html/template` escapes all task text.
- Redirect targets come from a fixed list, and notices are fixed keys, so no request text reaches a redirect or a message.
- Tokens are read only from the POST body and only forms are parsed, never the query string.

### D5: Actions without JavaScript

Every action is a form that carries `expected_state` and `expected_version`. Responses to a POST are redirects, so a
reload cannot repeat them. Before completing or skipping, the server reads the task. If it repeats and the request is
not confirmed, it redirects to a confirmation page that keeps the versions from the original click, so a change in
between is reported as a conflict. Quick add carries a random `Idempotency-Key` per rendered form, so a double tap
creates one task.

### D6: Languages

A small map per language in Go, used through a template function. A language cookie, validated against the supported
list, wins over `Accept-Language`; the fallback is English. A test checks that every key exists in every language and
that templates use only known keys.

## Risks / Trade-offs

- A lost phone keeps a session for up to 30 days and one session cannot be ended alone → remove the token and restart.
- A link from another site can show the sign-in page because of `SameSite=Strict` → accepted, it fails safe.
- A proxy without `ORGANON_CLIENT_IP_HEADER` makes all clients share one failure count, so failed sign-ins could block
  API clients → documented as required when the UI is exposed.
- The language cookie is `SameSite=Lax`, not Strict, so the language holds on links from elsewhere. It is not secret.
- The UI cannot undo a completion → repeating tasks ask first; the CLI can reopen other tasks.
