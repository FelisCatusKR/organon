# Security policy

Organon holds personal data and is meant to be reachable over the network, so security reports are welcome.

## Reporting a vulnerability

Please report privately through GitHub: **Security → Report a vulnerability** on this repository. Do not open
a public issue. Include what you did, what happened and which version or commit you ran (`organon version`).

You can expect a first answer within a week. This is a personal project maintained in spare time, so a fix
may take longer; you will be told what is planned.

## Supported versions

Before 1.0 only the latest commit on `main` is supported.

## Scope

Of particular interest:

- anything that makes the engine evaluate Lisp or run a program, for example through task titles or bodies,
  or through `.org` files it reads
- authentication or scope bypasses in the API, and token handling
- reading or writing files outside the data directory
- ways for the API container to reach the data directly

Running Organon without a TLS-terminating proxy in front of it on an untrusted network is a deployment choice,
not a vulnerability.
