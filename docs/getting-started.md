---
title: Getting Started
nav_order: 2
---

# Getting Started

## Installation

### Docker compose

Meerkat CRM ships as a single all-in-one image that bundles the frontend and backend into one container, published in the github registry as `ghcr.io/meerkatcrm/meerkat-crm`.
Images are available for linux/amd64 and linux/arm64.

You can use either the `:latest` tag or a specific version (e.g. `:0.9` or `:0.9.1`).

Copy the [sample docker compose file](https://github.com/meerkatcrm/meerkat-crm/blob/main/docker-compose.yml) as well as [sample env file](https://github.com/meerkatcrm/meerkat-crm/blob/main/.env.example) and rename the env file to `.env`.

After adjusting the environment variables as needed you can run:
```docker compose up -d```

### Environment variables

| Variable | Description |
|---|---|
| `JWT_SECRET_KEY` | Random string used for JWT signing (minimum 32 characters) |
| `ENCRYPTION_KEY` | Encrypts the stored CardDAV and calendar passwords. Generate with `openssl rand -base64 32`, keep it with your backups and never change it. Optional for now: without it an error is logged at startup. See [Encryption key](deployment.md#encryption-key) |
| `FRONTEND_URL` | Used for CORS headers. Wildcard (`*`) is allowed but not recommended for production use |
| `RESEND_API_KEY` | API key for [Resend](https://resend.com), used to send e-mail notifications. The generous free tier is more than enough for any personal setup |
| `RESEND_FROM_EMAIL` | Sender e-mail address for Resend, needs to be configured in Resend |
| `SMTP_HOST` | SMTP server hostname, used to send e-mail notifications via your own mail server (alternative or in addition to Resend) |
| `SMTP_PORT` | SMTP server port (default `587`; use `465` with `SMTP_USE_TLS=true`) |
| `SMTP_USERNAME` | SMTP auth username (leave empty for unauthenticated relays) |
| `SMTP_PASSWORD` | SMTP auth password |
| `SMTP_FROM_EMAIL` | Sender e-mail address for SMTP |
| `SMTP_USE_TLS` | Set to `true` for implicit TLS (port 465); otherwise STARTTLS is used |
| `CARDDAV_ENABLED` | When set to `true` the application acts as a CardDAV server which allows contacts to be synced with your phone |
| `DISABLE_REGISTRATION` | When set to `true`, new user registration is disabled (existing users can still log in). Default is `false` |
| `DATA_PATH` | Host directory where the database file should be stored |
| `PHOTOS_PATH` | Host directory where the contact photos should be stored |
| `JWT_EXPIRY_HOURS` | Session lifetime in hours. The session is renewed while you use the app (once less than half of it is left), so you only need to sign in again after this many hours of inactivity. Default is 96 hours (4 days) |
| `JWT_MAX_SESSION_HOURS` | Absolute session limit in hours, counted from sign-in. Sliding renewal never extends a session beyond it, so you must sign in again after this long even if you use the app daily. Must be at least `JWT_EXPIRY_HOURS`. Default is 720 hours (30 days), or `JWT_EXPIRY_HOURS` if that is larger |
| `OIDC_PROVIDER_URL` | Issuer URL of your OIDC provider (i.e. endpoint URI for your provider). Required to enable SSO |
| `OIDC_CLIENT_ID` | OAuth2 client ID registered with your OIDC provider |
| `OIDC_CLIENT_SECRET` | OAuth2 client secret registered with your OIDC provider |
| `OIDC_PROVIDER_NAME` | Display name of your provider shown on the SSO login button (e.g. `Pocket ID`, `Keycloak`). Defaults to the host of `OIDC_PROVIDER_URL` |
| `OIDC_AUTO_PROVISION` | When `true`, a new account is automatically created on first SSO login. Default is `false` |
| `OIDC_TRUST_EMAIL` | When `true`, skips the `email_verified`  requirement when linking an OIDC identity to an existing account by email. Safe to enable for self-hosted providers (e.g. Authentik) where you control all user accounts. Default is `false` |
| `DISABLE_PASSWORD_LOGIN` | When `true` (and OIDC is enabled), sign-in is SSO-only: the login page shows just the SSO button, and password login, password reset and registration are refused by the server. Ignored while OIDC is off, so it cannot lock everyone out. CardDAV still authenticates with account passwords. Default is `false` |
| `REMINDER_TIME` | Time of day at which reminder emails are sent, in `HH:MM` format (24-hour). Default is `12:00` |
| `REMINDER_TIMEZONE` | Timezone used for scheduling reminder emails. Must be a valid [IANA timezone name](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones) (e.g. `Europe/Berlin`). Default is `UTC` |
| `BLOCK_PRIVATE_URLS` | When set, decides for webhooks, calendar and contact sync and the Monica import whether URLs that resolve to private, loopback or link-local addresses are refused (SSRF protection), and overrides the per-feature `*_BLOCK_PRIVATE_URLS` flags. Recommended for any instance with more than one user. A value that is not true/false/1/0/yes/no/on/off counts as `true`. Default: unset (the per-feature flags apply, all `false`) |
| `MONICA_BLOCK_PRIVATE_URLS` | When set to `true`, Monica imports refuse URLs that resolve to private or loopback addresses (SSRF protection for cloud or multi-tenant setups). Default is `false` |
| `AUTH_RATE_LIMIT_BURST` | Per-IP burst limit for the login, registration, password reset and SSO endpoints. Leave unset in production, as raising it weakens brute-force protection. Default is `50` |
| `API_RATE_LIMIT_BURST` | Per-IP burst limit for the authenticated API. Leave unset in production. Default is `500` |

SSO is disabled unless all three of `OIDC_PROVIDER_URL`, `OIDC_CLIENT_ID`, and `OIDC_CLIENT_SECRET` are set.

Other variables are found in the [sample env file](https://github.com/meerkatcrm/meerkat-crm/blob/main/.env.example).

The container runs as a non-root user (UID/GID 1001), and nothing inside it runs as root: there is no startup script to `chown` anything. Whatever user the container runs as must own the mounted data and photo directories, so create them owned first:

```bash
mkdir -p data photos && chown -R 1001:1001 data photos
```

Run `id` on your host to find your own UID and GID and set them as `PUID`/`PGID` in your `.env` file to have the folders owned by your host user instead (optional).

## Post-Installation Setup

When running Meerkat-CRM you can access the application under the specified port (default is `7300`). 
To get started you need to register a user. The first user will automatically receive administrator rights and therefore be able to access the admin panel in the settings menu.

## Backup

Make regular backups of your data by copying the database file in your data directory as well as the contents of the photo directory to a separate device.
