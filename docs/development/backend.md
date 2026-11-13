---
title: Backend Development
parent: Development
nav_order: 2
---

# Backend Development

## Setup

Requires current [Go version](https://go.dev/doc/install).

```sh
cd backend
cp .env.example .env   # then edit with your values
go mod tidy
```

## Running Locally

```sh
source .env
go run main.go
```

The server starts on `PORT` (default `8080`). Migrations run automatically.

## Adding a New Endpoint

1. Define an input DTO in `models/` with validation tags.
2. Add a controller function in `controllers/`.
3. Register the route in `routes/routes.go`.
4. Add the validation schema to the middleware registration if needed.

## Database Migrations

### Creating a Migration

```sh
make migrate-create NAME=add_foo_column
```

This creates two files in `database/migrations/`: `NNNNNN_add_foo_column.up.sql` and `.down.sql`.

### Search index (FTS5)

Search uses the SQLite FTS5 tables `contacts_fts`, `notes_fts` and `activities_fts` (migration 41). Each row's rowid is the id of the indexed row, `AFTER INSERT/UPDATE/DELETE` triggers keep them in sync, and the `unicode61 remove_diacritics 2` tokenizer folds case and accents. The index holds no `user_id`: queries always join back to the real table, which filters `user_id` and `deleted_at`. `ftsMatch` in `controllers/helpers.go` quotes every typed word as a prefix term, so input is never FTS5 syntax, and returns `ok == false` for phone-like digits, text in scripts without spaces between words (`unspacedScripts`: CJK, Thai, Lao, Khmer, Myanmar, Tibetan) and punctuation, which keep the `LIKE` search.

- **Recreating `contacts`, `notes` or `activities`** in a migration drops their triggers. Recreate them and rebuild the index (`DELETE FROM contacts_fts;` and the `INSERT ... SELECT` from migration 41). `TestSearchIndexTriggersSurviveAllMigrations` fails until you do. The same applies when you add a column to an index.
- Controller tests build their database with `AutoMigrate`, so `setupRouter` also runs the migration 41 SQL.

### Running Migrations

Migrations run automatically on startup. To apply or roll back manually:

```sh
make migrate-up
make migrate-down
make migrate-status
```

## Models and DTOs

GORM models (`Contact`, `Activity`, etc.) live alongside input DTOs (`ContactInput`, etc.) in `models/`. DTOs are what controllers receive after validation while models are what GORM persists.

All models include `UserID uint` for tenant isolation.

## Services

Complex business logic lives in `services/`. Controllers call services; services own multi-step operations (e.g. sending emails). Services receive a `*gorm.DB` and any needed config, they do not access `*gin.Context`.

## Time

Business code never reads the wall clock, so time-dependent behaviour can be tested by passing a time and previewed with `FAKE_NOW` (see [deployment](../deployment.md#fake-clock-for-demos-and-tests)).

- `backend/clock` is the only package that reads it: `clock.Real()`, `clock.NewFake(t)` for tests, and the clock built from `FAKE_NOW`.
- `middleware.ClockMiddleware` takes the time once per request. Handlers read it with `middleware.RequestNow(c)` and pass `now time.Time` down to services.
- Scheduling stays on the wall clock (`clock.Real()`): job locks, the delivery queue's due, claim and retry times, and login lockouts. They decide *when* something happens, and rows stamped by a fake clock would block jobs or strand deliveries once the clock changes.
- Work that outlives the request or does slow I/O per item (calendar and contact sync, the reminder job, webhook payloads) takes a `clock.Clock` instead (`middleware.RequestClock(c)` in a handler, the clock from `main.go` in a scheduled job) and reads it where it stamps something: a time taken before a slow HTTP call is stale after it.
- GORM's `NowFunc` and JWT expiry use the same clock.
- `forbidigo` fails the lint on `time.Now` outside `main.go`, `clock/`, `middleware/`, `metrics/`, `cmd/` and tests. A line that must use the real clock (an I/O deadline, a duration) carries `//nolint:forbidigo // reason`. `time.Since` and `time.Until` measure durations and are allowed.

## Testing

```sh
go test ./...
```

Tests use an in-memory SQLite database (auto-migrated). See [Testing](testing.md).
