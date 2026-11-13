---
title: Deployment
nav_order: 6
has_children: false
---

# Deployment

For initial setup see [Getting Started](getting-started.html). This page covers production-specific concerns.

## How the Docker Setup Works

Meerkat CRM runs as a single all-in-one container. Inside it, nginx serves the React SPA and proxies all `/api/`, `/carddav/`, and `/.well-known/carddav` requests to the Go backend on `127.0.0.1:8080`; the backend is never exposed to the host directly. Only the nginx port is published (default `7300`). This same-origin proxy is built in, so nothing extra is required.

You only need an external reverse proxy for TLS termination. Point it at the published port (default `7300`):

```nginx
server {
    listen 443 ssl;
    server_name meerkat.example.com;

    location / {
        proxy_pass http://localhost:7300;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

## Production Environment

Set these variables in `.env` when running over HTTPS:

| Variable | Value |
|---|---|
| `FRONTEND_URL` | Exact origin, e.g. `https://meerkat.example.com` (never `*`) |
| `COOKIE_SECURE` | `true` |
| `COOKIE_DOMAIN` | Leave empty. Only set it (e.g. `.example.com`) if frontend and API live on different subdomains |
| `JWT_SECRET_KEY` | Generate with `openssl rand -base64 32` |
| `ENCRYPTION_KEY` | Generate with `openssl rand -base64 32`; a different value from `JWT_SECRET_KEY` |

## Encryption key

`ENCRYPTION_KEY` encrypts the CardDAV and calendar passwords stored in the database. Before it,
they used a key derived from `JWT_SECRET_KEY`, so changing the JWT secret made them unreadable.

- **Make one:** `openssl rand -base64 32`. Meerkat refuses to start when the value is not 32 bytes
  in base64, looks typed by hand (few distinct bytes) or equals `JWT_SECRET_KEY`.
- **Keep it** with your `.env` and your backups. It is never written to the database or the data
  directory, so a backup alone cannot decrypt the passwords, and without the key they are lost.
- **First start with it:** Meerkat encrypts all old passwords again with the new key, in one
  transaction, and logs how many it moved. A password that cannot be decrypted stays as it is and
  is counted in the log; its user must enter it again. A restored old backup is moved the same way.
  An older Meerkat version cannot read the moved passwords.
- **Wrong or missing key:** Meerkat stores a fixed text encrypted with the key. When it does not
  decrypt, or when the database holds values of the new key and `ENCRYPTION_KEY` is unset, the
  server stops with the steps to fix it.
- **Lost key:** set a new `ENCRYPTION_KEY` and `ENCRYPTION_KEY_RESET=true`, start Meerkat once,
  then remove `ENCRYPTION_KEY_RESET`. The passwords of the lost key are deleted and users enter
  them again. With the right key the reset does nothing.
- **Without it** Meerkat starts and logs an error with these steps. A future version will require
  it. `JWT_SECRET_KEY` can change once the passwords use `ENCRYPTION_KEY`.


## Single Sign-On (OIDC)

Meerkat CRM supports SSO via any OpenID Connect provider (Keycloak, Google, Authentik, Authelia, etc.). When enabled, a **Sign in with provider** button appears on the login page.

### Setup

1. Register a new OAuth2 client with your provider. Set the redirect URI to:
   ```
   https://meerkat.example.com/api/v1/auth/oidc/callback
   ```
   This is derived automatically from `FRONTEND_URL`, no separate variable needed.

2.  Set the OIDC environment variables in the docker compose. See [Getting-Started → Environment variables](getting-started.html#environment-variables) for details. SSO is disabled if any of the first three variables are missing.

### Account linking

On first SSO login, the backend attempts to match the OIDC identity to an existing account in this order:

1. **Subject match** — if the user has logged in via this provider before, their account is found directly.
2. **Email match** — if the provider returns a *verified* email that matches an existing account, the OIDC identity is linked to that account automatically. Unverified emails are ignored to prevent account takeover (except if `OIDC_TRUST_EMAIL=true` is set).
3. **Auto-provision** — if `OIDC_AUTO_PROVISION=true` and no account matched, a new account is created using the email/name from the provider.

If auto-provisioning is disabled and no match is found, the user sees an error and must be registered manually first.

### Passwords

Accounts created through SSO have no password and can only log in via SSO. Existing password-based accounts that get linked retain their password.

## Security headers

Meerkat sets its security headers itself; a reverse proxy in front should pass them through and
**not add its own** (a second `Content-Security-Policy` is combined with the first and can only make
the policy stricter, a second `Strict-Transport-Security` or `X-Frame-Options` is sent twice).

| Response | Headers |
|---|---|
| The web app (HTML, JS, CSS, images), sent by nginx | `Content-Security-Policy` (below), `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy` (no camera, microphone, geolocation, payment, USB), `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin` |
| The API (`/api/`, `/carddav/`, downloads, photos), sent by the backend | the same, but `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'` and `Cross-Origin-Resource-Policy: same-site` |

The web app policy is enforced, and the E2E suite fails on any violation:

```
default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:;
font-src 'self' data:; connect-src 'self'; manifest-src 'self'; worker-src 'self'; object-src 'none';
base-uri 'self'; form-action 'self'; frame-ancestors 'none'
```

It lives in one file, `frontend/security-headers.conf`, which the all-in-one image and the separate
frontend image both include in every location that serves files (never in the proxied ones, where the
backend's headers pass through). `frontend/src/securityHeaders.test.ts` fails when a config drifts.

**HSTS** is sent only when the browser is on HTTPS: nginx looks at `X-Forwarded-Proto` (set it to
`https` in your TLS proxy; only `http` and `https` are accepted) and sends
`Strict-Transport-Security: max-age=31536000`. The backend sends the same when the request came over
TLS or with `X-Forwarded-Proto: https`, and adds `includeSubDomains` only when you declared HTTPS
yourself with `COOKIE_SECURE=true`. Plain-HTTP access never gets it.

**API on another origin.** Set `FRONTEND_URL` to the web app's origin as before (CORS and the
cookie keep working). Serve the API on the same site as the web app, for example
`app.example.com` and `api.example.com`: the backend sends `Cross-Origin-Resource-Policy: same-site`,
and the session cookie is `SameSite=Lax`, so photos from another site would not load anyway. Then
add the API origin to the web app's policy, for example `connect-src 'self' https://api.example.com`
and `img-src 'self' data: blob: https://api.example.com`, by editing `frontend/security-headers.conf`
and rebuilding, or by mounting your own copy over `/etc/nginx/security-headers.conf`. Never add
`'unsafe-inline'` or `'unsafe-eval'` to `script-src`.

**Gap:** responses that nginx generates itself (its error pages while the backend is down, the
`/.well-known/carddav` redirect) carry no security headers. They contain no application data.

## Outgoing requests

Meerkat makes requests to URLs that users choose: webhooks, calendar (CalDAV/ICS) and contact
(CardDAV) sync, the Monica import, and image URLs (the profile picture dialog, photo URLs in vCards,
Monica gravatars). Every one of them dials through one guard (`backend/netguard`), which checks the
address it connects to, also after a redirect or a changed DNS answer, and never uses `HTTP_PROXY`.

**Private addresses.** `BLOCK_PRIVATE_URLS=true` refuses private, loopback and link-local addresses
for webhooks, sync and the Monica import. Set it on any instance with more than one user; Meerkat
logs a warning at startup when it is off there. It is off by default because a LAN server
(Nextcloud, Radicale, a local Monica) is a common setup. While it is unset, the older
`WEBHOOK_`, `CALDAV_`, `CARDDAV_` and `MONICA_BLOCK_PRIVATE_URLS` flags apply per feature.

**Image URLs** are always refused when they point to an internal address, whatever
`BLOCK_PRIVATE_URLS` says. The decision behind the remaining rules:

- Only `http` and `https` on ports 80 and 443, also for each redirect (at most two).
- At most four image fetches run at a time on the server; others wait. Each has 15 s and 10 MB.
- The image proxy allows 30 requests per minute per user (burst 5) and returns the image decoded
  and encoded again as JPEG, never the remote bytes.

What remains: a signed-in user can make the server request public hosts on ports 80 and 443,
slowly, and see whether the request returned an image. A private CardDAV server's photo URLs do not
load. An allow-list of hosts is not possible, since users paste image URLs from any site.

## Delivery queue (webhooks and email)

Webhook deliveries and emails (reminders, password resets) go through a database-backed queue, so a
restart, a crash or a failing receiver or mail server does not lose them.

- The row is written first, then sent at once in the background. A job runs every minute and sends
  everything that is due, including rows a restart interrupted. A claim older than 5 minutes (the
  process died while sending) is taken over, so a delivery can in rare cases arrive twice. Sends
  have timeouts well below that (HTTP 15 s, SMTP and Resend 30 s each). Webhook payloads carry a
  stable `id`; use it to deduplicate.
- A failed try is repeated after 1 minute, 5 minutes, 30 minutes and 2 hours. After the 5th failed
  try the entry is **dead** (dead letter) and not tried again. A webhook whose URL points to a
  blocked private address is dead at once, and so are entries of a webhook that was deleted or
  switched off, or whose owner was deleted.
- Webhook entries show up per webhook under Settings, Webhooks, Recent Deliveries: delivered (HTTP
  status), retrying (with the next try time) or gave up. There is one entry per event, updated on
  every try.
- Emails: while no email channel is configured nothing is sent and no try is counted; the entries
  wait. The body of an email is removed as soon as it is sent or dead. Password reset emails are not
  retried after the link expires (1 hour). Deleting a user deletes their queued emails.
- Delivered entries are purged after 7 days and dead ones after 30, once an hour.
- Reminders are marked as emailed when the email is queued, not when it is delivered; the queue
  does the retrying.
- With metrics on: `meerkat_outbox_pending{kind}` and `meerkat_outbox_dead{kind}` (gauges) and
  `meerkat_outbox_dead_total{kind}` (counter); `kind` is `webhook` or `email`. Alert on
  `meerkat_outbox_dead > 0` for 10 minutes, or `increase(meerkat_outbox_dead_total[1h]) > 0`.

## Monitoring with Prometheus

Meerkat can expose metrics in the Prometheus text format. It is **off by default**. Set `METRICS_ENABLED=true` and choose how to protect it:

| Variable | Meaning |
|---|---|
| `METRICS_ENABLED` | `true` to serve `/metrics`. Default `false` (no route exists) |
| `METRICS_TOKEN` | If set, scrapers must send `Authorization: Bearer <token>` (compared in constant time) |
| `METRICS_ADDR` | If set (for example `127.0.0.1:9090` or `:9090` on a private Docker network), `/metrics` is served only on this extra listener and not on the main port |

**Where the endpoint is.** The nginx in the images proxies only `/api/`, `/health` and `/carddav/`; every other path, `/metrics` included, is answered with the web app (`index.html`), so the browser port never serves metrics. How to reach them depends on the layout:

- *Single container* (`docker-compose.yml`, service `meerkat`): nginx listens on 8080 and the backend on `PORT=8081` inside the container. Scrape `meerkat:8081/metrics` from a container on the same Docker network (port 8081 is not published to the host, which is what you want), or set `METRICS_ADDR=:9090` and scrape `meerkat:9090/metrics`.
- *Legacy split* (`docker-compose.legacy.yml`: `backend` and `frontend` containers): the backend listens on 8080, scrape `backend:8080/metrics`.
- *Without Docker*: `/metrics` is on the backend `PORT`.

If your own reverse proxy forwards everything to the backend, set a token or use `METRICS_ADDR`, or block `/metrics` there. Without a token or `METRICS_ADDR`, Meerkat logs a warning at startup. If `METRICS_ADDR` cannot be bound, Meerkat does not start. The metrics contain no user data (routes are patterns such as `/api/v1/contacts/:id`, never URLs), but they reveal traffic and failure patterns.

| Metric | Type | Meaning |
|---|---|---|
| `meerkat_http_requests_total{method,route,status}` | counter | Requests by route pattern (`unmatched` for 404s without a route) |
| `meerkat_http_request_duration_seconds{method,route}` | histogram | Latency |
| `meerkat_job_last_success_timestamp_seconds{job}` | gauge | When the job last completed a run. Jobs: `reminders`, `calendar-sync`, `contact-sync` (the ones kept in `job_executions`). Missing until the first run after upgrading to this version has completed; a run that failed or was cut short never sets it |
| `meerkat_job_running{job}` | gauge | 1 while the job holds its lock. A process that died mid-run leaves the lock until the next run takes it over (after 5 minutes), so it can stay 1 for a while |
| `meerkat_job_runs_total{job,result}` | counter | Runs since the process started, same job names. `result` is `success`, `failure`, or `skipped` (turned away by the rate limit or another instance's lock; the sync jobs count `success` when they ran, per-connection errors are in their own status and logs) |
| `meerkat_webhook_deliveries_total{result}` | counter | Webhook delivery attempts (every retry counts) |
| `meerkat_emails_total{result}` | counter | Emails where at least one channel succeeded (`success`) or all failed (`failure`) |
| `meerkat_outbox_pending{kind}` | gauge | Webhook deliveries and emails waiting for a try or retry (`kind` is `webhook` or `email`) |
| `meerkat_outbox_dead{kind}` | gauge | Entries that gave up and are still stored |
| `meerkat_outbox_dead_total{kind}` | counter | Entries that gave up since the process started |
| `go_goroutines`, `go_memstats_alloc_bytes`, `process_start_time_seconds` | gauge | Process basics |

Example scrape config:

```yaml
scrape_configs:
  - job_name: meerkat
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/meerkat-token
    static_configs:
      # single container: meerkat:8081 (or the METRICS_ADDR host and port); legacy split: backend:8080
      - targets: ["meerkat:8081"]
```

Alert ideas (adapt the thresholds):

- Backend down: `up{job="meerkat"} == 0` for 5 minutes.
- Reminders did not run for over 26 hours: `time() - meerkat_job_last_success_timestamp_seconds{job="reminders"} > 26 * 3600`. The same for `calendar-sync` and `contact-sync`, with your sync interval.
- A scheduled job failed: `increase(meerkat_job_runs_total{result="failure"}[1h]) > 0` (`skipped` is normal after restarts).
- Email failing: `increase(meerkat_emails_total{result="failure"}[1h]) > 0`.
- Many failing webhooks: `increase(meerkat_webhook_deliveries_total{result="failure"}[1h]) > 10`.
- Server errors: `sum(rate(meerkat_http_requests_total{status=~"5.."}[5m])) > 0.05`.
- Slow API: `histogram_quantile(0.95, sum by (le) (rate(meerkat_http_request_duration_seconds_bucket[5m]))) > 1`.

Counters start at zero on every restart and the metrics are per instance. Job run counters of a job that never ran do not exist yet; use `absent()` with care.

## Photo files

Profile photos and their 48 px thumbnails are files in `PROFILE_PHOTO_DIR`
(`<id>_photo.jpg` and `<id>_thumb.jpg`); back the directory up with the database. Before
migration 000040 the thumbnails were base64 in the `contacts` table. At startup Meerkat writes each
of those into a file (safe to repeat; it logs how many it moved). The old column stays untouched,
so rolling back loses nothing; a later release drops it. Contact lists carry a thumbnail URL with
the file name as version, which the browser caches.

## Fake clock for demos and tests

`FAKE_NOW` (an RFC 3339 time such as `2026-12-24T12:00:00Z`) starts the server clock at that instant and lets it run, so a demo or a manual test can walk through time: a birthday next week, a reminder due tomorrow, a password reset link about to expire. **Do not set it on a real instance.** The server logs a warning at startup. While it is set:

- the delivery queue purge is skipped, because a clock set into the future would delete data for good;
- the schedule of the background jobs still follows the real time: the fake clock changes what a job does, not when it starts;
- stored timestamps (`created_at`, `updated_at`, sync times) use the fake time, so contact sync against a real CardDAV server compares real remote revisions with fake local ones;
- the browser does not follow it: dates the web app computes itself (the default date of a new note, "overdue") use the browser clock.

How the clock is wired is described in [Backend development](development/backend.md#time).

## Upgrades

```sh
docker compose pull
docker compose up -d
```

Database migrations run automatically on startup.

## Backups

Back up the SQLite database file and photo directory:

```sh
cp /path/to/data/meerkat.db /backups/meerkat-$(date +%F).db
rsync -a /path/to/photos/ /backups/photos/
```

The database can be copied while the app is running (SQLite WAL mode).
