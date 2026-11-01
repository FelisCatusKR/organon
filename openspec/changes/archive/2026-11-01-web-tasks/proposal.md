## Why

Organon can only be used from a terminal today, so it is out of reach on a phone. People need to see what matters
today, act on it and note down a new task from a browser. This is the first half of the web interface (#25): tasks
and sign-in. Notes follow in a separate change, `web-notes`.

## What Changes

- The API binary can serve a server-rendered web UI at the root of its origin, turned on with
  `ORGANON_WEB_ENABLED=true`. It is off by default, and with it off nothing about the service changes.
- Signing in exchanges an API token for a session cookie. There are no web passwords or accounts.
- Pages for today, all open tasks and waiting tasks; start, complete and skip; quick add.
- English UI with Korean as the first translation, chosen by `Accept-Language` and a language switch.
- **BREAKING** (for the container interface only): two new optional variables, `ORGANON_WEB_ENABLED` and
  `ORGANON_WEB_INSECURE_COOKIE`. Existing deployments keep working unchanged.

## Capabilities

### New Capabilities

- `web-ui`: signing in, sessions, task pages and browser-facing security of the web interface.

### Modified Capabilities

- `api-access`: the token requirement now names what may answer without a token when the UI is on.

## Impact

- New package `api/internal/web` with embedded templates and CSS. No new Go dependencies, no JavaScript, no Node.
- `api/internal/httpapi`: mounts the UI and offers an in-process way to call the API as a given token.
  `api/openapi.yaml` does not change.
- `contrib/container-interface.json`, `compose.yaml`, `docs/`.
