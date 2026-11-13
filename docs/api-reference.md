---
title: API Reference
nav_order: 8
has_children: false
---

# API Reference

A machine-readable OpenAPI 3.1 document of the endpoints converted so far is in
[openapi.json](openapi.json). It is generated from the code, so it is always current; the
rest of this page is written by hand.

## Base URL

```
/api/v1
```

## Authentication

Auth endpoints (`/login`, `/register`, `/logout`, `/password-reset/*`) are public. All other endpoints require authentication.

Admin endpoints (`/admin/*`) additionally require the user to have the admin flag set.

Login sets an httpOnly JWT cookie. Subsequent requests must include it (i.e. send with `credentials: 'include'`). The cookie name and domain are configured via `COOKIE_DOMAIN` and `COOKIE_SECURE`.

Sessions are sliding: when a request authenticated by the cookie arrives with less than half of `JWT_EXPIRY_HOURS` left, the response carries a fresh `auth_token` cookie. The renewal stops at `JWT_MAX_SESSION_HOURS` after login, and also once the user has been deleted or has changed their password. A session already issued keeps working until it expires. Bearer tokens (JWT or API) are never refreshed.

Alternatively, API tokens can be used in the `Authorization` header:

```
Authorization: Bearer meerkat_<token>
```

API tokens are created and managed via the admin endpoints, the tokens always have normal user rights. The plaintext token is only returned once at creation time.

## Error Responses

All errors follow the same structure:

```json
{
  "code": "NOT_FOUND",
  "message": "Contact not found",
  "details": {}
}
```

Common error codes:

| Code | HTTP status |
|---|---|
| `UNAUTHORIZED` | 401 |
| `INVALID_CREDENTIALS` | 401 |
| `TOKEN_EXPIRED` | 401 |
| `FORBIDDEN` | 403 |
| `NOT_FOUND` | 404 |
| `VALIDATION_ERROR` | 400 |
| `INVALID_INPUT` | 400 |
| `ALREADY_EXISTS` | 409 |
| `RATE_LIMIT_EXCEEDED` | 429 |
| `INTERNAL_ERROR` | 500 |

## Request IDs

Every request and response carries an `X-Request-ID` header created by the middleware.

---

## Endpoints

### Auth

| Method | Path | Description |
|---|---|---|
| `POST` | `/register` | Create a new user account |
| `POST` | `/login` | Authenticate and set session cookie |
| `POST` | `/logout` | Clear session cookie |
| `POST` | `/check-password-strength` | Validate a password without registering |
| `POST` | `/password-reset/request` | Send a password reset email |
| `POST` | `/password-reset/confirm` | Apply a password reset token |

### Users

| Method | Path | Description |
|---|---|---|
| `GET` | `/users/me` | Get the current user |
| `POST` | `/users/change-password` | Change password |
| `PATCH` | `/users/language` | Update UI language preference |
| `PATCH` | `/users/date-format` | Update date format preference |
| `GET` | `/users/custom-fields` | Get custom field names |
| `PATCH` | `/users/custom-fields` | Update custom field names |

### Contacts

| Method | Path | Description |
|---|---|---|
| `GET` | `/contacts` | List contacts (supports search and circle filter). `search` matches name, nickname, e-mail addresses and phone numbers, and the names of relationships without a contact of their own: case and accents are ignored (`zoe` finds `Zoë`), and every word must match the start of a word, in any order (`mül zo`). Without `sort` the best match comes first. Terms with only digits and phone punctuation (`795550`), with no letters or digits, or in Chinese, Japanese or Korean are substring searches. `search` on `/notes` and `/activities` matches the same way but keeps date order |
| `POST` | `/contacts` | Create a contact |
| `GET` | `/contacts/:id` | Get a contact (supports filtering the returned fields) |
| `PUT` | `/contacts/:id` | Update a contact |
| `DELETE` | `/contacts/:id` | Delete a contact |
| `POST` | `/contacts/:id/archive` | Archive a contact |
| `POST` | `/contacts/:id/unarchive` | Unarchive a contact |
| `GET` | `/contacts/circles` | List all circles in use |
| `GET` | `/contacts/random` | Get five random contacts |
| `GET` | `/contacts/birthdays` | Get upcoming birthdays |
| `POST` | `/contacts/:id/profile_picture` | Upload a profile picture (multipart) |
| `GET` | `/contacts/:id/profile_picture` | Get a contact's profile picture |
| `GET` | `/proxy/image` | Proxy an external image URL for upload preview |

### Relationships

| Method | Path | Description |
|---|---|---|
| `GET` | `/contacts/:id/relationships` | List outgoing relationships |
| `GET` | `/contacts/:id/incoming-relationships` | List incoming relationships |
| `POST` | `/contacts/:id/relationships` | Create a relationship |
| `PUT` | `/contacts/:id/relationships/:rid` | Update a relationship |
| `DELETE` | `/contacts/:id/relationships/:rid` | Delete a relationship |

### Employment History

| Method | Path | Description |
|---|---|---|
| `GET` | `/contacts/:id/employment-history` | List entries, current and most recent first |
| `POST` | `/contacts/:id/employment-history` | Create an entry |
| `PUT` | `/contacts/:id/employment-history/:eid` | Update an entry |
| `DELETE` | `/contacts/:id/employment-history/:eid` | Delete an entry |
| `POST` | `/contacts/:id/employment-history/:eid/end` | End a current entry (sets `end_date` to the optional `date` query parameter, `YYYY-MM-DD`, or else today in UTC); `400` if already ended |

`POST` / `PUT` body:

```json
{
  "organization": "Acme Inc.",
  "department": "Engineering",
  "job_title": "Developer",
  "role": "Team lead",
  "start_date": "2021-03",
  "end_date": "",
  "notes": ""
}
```

At least one of `organization`, `department`, `job_title` or `role` is required (entries backfilled from a contact with only a job title have no organization). `start_date` and `end_date` accept `YYYY`, `YYYY-MM` or `YYYY-MM-DD`; an empty `end_date` marks the entry as current, and `end_date` must not be before `start_date`. Responses wrap the entry (or list) in an `employment_history` key.

Every change recomputes the contact's `organization`, `department`, `job_title` and `role` from the current entry with the latest `start_date`. Entries can be embedded in `GET /contacts` with `includes=employment_history`, and are included in `GET /contacts/:id` as `employment_histories` (not when the `fields` filter is used). Webhook events: `employment_history.created`, `employment_history.updated`, `employment_history.deleted`.

Creating a contact with these fields, `PUT /contacts/:id`, importing, or a CardDAV sync with non-empty values updates the current entry, or creates one if none exists. Changing a value overwrites the current entry, so to record a job change and keep the previous job, end the entry and create a new one through the endpoints above.

`PUT /contacts/:id` keeps the stored value of any of these fields that is blank or missing, so it can't clear them. Importing and CardDAV sync replace all four unless all four are blank. Blanking all four does not end or delete the entry: the values are re-derived from it (end or delete the entry instead).

`PUT /contacts/:id`, CardDAV and remote address book sync treat values identical to the most recently ended entry as stale: the values are re-derived from the current entry, or emptied if there is none, and no entry is created or changed. To reopen a job through the API, create a new entry through the endpoints above.

### Notes

| Method | Path | Description |
|---|---|---|
| `GET` | `/contacts/:id/notes` | List notes for a contact |
| `POST` | `/contacts/:id/notes` | Create a note for a contact |
| `GET` | `/notes` | List unassigned notes |
| `POST` | `/notes` | Create an unassigned note |
| `GET` | `/notes/:id` | Get a note |
| `PUT` | `/notes/:id` | Update a note |
| `DELETE` | `/notes/:id` | Delete a note |

### Activities

| Method | Path | Description |
|---|---|---|
| `GET` | `/activities` | List all activities |
| `POST` | `/activities` | Create an activity |
| `GET` | `/activities/:id` | Get an activity |
| `PUT` | `/activities/:id` | Update an activity |
| `DELETE` | `/activities/:id` | Delete an activity |
| `GET` | `/contacts/:id/activities` | List activities for a contact |

### Reminders

| Method | Path | Description |
|---|---|---|
| `GET` | `/reminders` | List all reminders |
| `GET` | `/reminders/upcoming` | List upcoming reminders (used by dashboard) |
| `GET` | `/reminders/:id` | Get a reminder |
| `PUT` | `/reminders/:id` | Update a reminder |
| `DELETE` | `/reminders/:id` | Delete a reminder |
| `POST` | `/reminders/:id/complete` | Mark a reminder complete (creates timeline entry) |
| `GET` | `/contacts/:id/reminders` | List reminders for a contact |
| `POST` | `/contacts/:id/reminders` | Create a reminder for a contact |
| `GET` | `/contacts/:id/reminder-completions` | List completion history for a contact (timeline entries) |
| `DELETE` | `/reminder-completions/:id` | Delete a completion entry |

### Import

| Method | Path | Description |
|---|---|---|
| `POST` | `/contacts/import/upload` | Upload a CSV file, returns parsed preview data |
| `POST` | `/contacts/import/preview` | Apply column mapping, returns contacts with duplicate detection |
| `POST` | `/contacts/import/confirm` | Execute the import with per-row decisions |
| `POST` | `/contacts/import/vcf/upload` | Upload a VCF file, returns contacts with duplicate detection |
| `POST` | `/contacts/import/vcf/confirm` | Execute the VCF import |

### Export

| Method | Path | Description |
|---|---|---|
| `GET` | `/export` | Download all data as CSV |
| `GET` | `/export/vcf` | Download all contacts as VCF (includes photos) |

### Network

| Method | Path | Description |
|---|---|---|
| `GET` | `/graph` | Get contact network graph data |

### API Tokens

| Method | Path | Description |
|---|---|---|
| `GET` | `/admin/api-tokens` | List all API tokens for the current user |
| `POST` | `/admin/api-tokens` | Create an API token — returns the plaintext token once |
| `DELETE` | `/admin/api-tokens/:id` | Revoke an API token |

`POST /admin/api-tokens` body:

```json
{ "name": "my token" }
```

Response includes `token` (the `meerkat_…` plaintext value) only on creation. Subsequent list responses omit it.

### Admin

| Method | Path | Description |
|---|---|---|
| `GET` | `/admin/users` | List all users |
| `GET` | `/admin/users/:id` | Get a user |
| `PATCH` | `/admin/users/:id` | Update a user (e.g. set admin flag) |
| `DELETE` | `/admin/users/:id` | Delete a user |

### Health

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Health check (no auth, no versioning). Accessible directly (without prepending base URL). |
