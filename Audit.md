# Code audit

Written 2026-11-10 on branch `chore/code-audit`, against `main` at `1fda220`.

Scope: all non-test backend and frontend source. Small fixes were applied on the
branch and are listed in part 1. Everything bigger is in part 2, most important first.

How to read the confidence of a finding:

- **Verified** means I reproduced it or read the code myself.
- **Reported** means a review pass found it by reading the code and cited the lines,
  but I did not re-check it. Confirm before acting.

Nothing here was tried in the running app. Backend `go vet`, `go test` and
`golangci-lint`, and frontend `tsc`, Vitest and ESLint all pass on the branch.

---

## Part 1: Fixed on this branch

### Security

**SQL injection through `:id` path parameters (verified, reproduced).**
GORM runs a non-numeric string passed as an inline condition as raw SQL, and about
30 handlers did `db.Where("user_id = ?", userID).First(&row, c.Param("id"))`.
Any logged-in user could send `GET /api/v1/contacts/EXISTS(SELECT …)` and read
other users' data one bit at a time, including password hashes and API token hashes.
Registration is open by default, so "logged-in" is a low bar.

`employment_history_controller.go` already carried a comment about this trap and
bound its ids correctly; the fix never reached the other controllers.

- Every such call in `controllers/` now binds the id: `First(&row, "id = ?", id)`.
- New `middleware.NumericIDParamsMiddleware` on `/api/v1` rejects any path parameter
  that is not a positive integer, so a future handler that forgets is still safe.
- Regression tests: `TestContactHandlers_TreatANonNumericIDAsAValue` (fails on `main`)
  and `TestRoutes_NonNumericIDsAreRejected`.

This repository is public. Consider shipping the fix and publishing an advisory
before this file or the branch is pushed.

**Panic text no longer returned to clients.** `ErrorHandlerMiddleware` put the panic
value in `details.panic`. It now stays in the log only.

### Backend bugs

| Where | What was wrong | Fix |
|---|---|---|
| `carddav/backend.go` (get and delete) | The legacy numeric-id fallback used `Sscanf("%d")`, which accepts any string that starts with digits. A request for an unknown UID such as `42f1e0c2-….vcf` served or **deleted** the user's contact 42. | The whole name must parse as a number. |
| `user_controller.go` language, date format, change password | The caller was looked up by the `username` claim of the session token. After an admin renamed the account the claim was stale, and if the old name was reused the write went to the other account. | Looked up by user id. Language and date format are single-column writes. API tokens can now reach these three endpoints; before, they got a 401 by accident. |
| `contact_controller.go` `DeleteContact` | Relationships of other contacts kept pointing at the deleted contact. | `related_contact_id` is cleared in the same transaction; the relationship keeps its name and type. |
| `contact_controller.go` `GetContacts` | The count query's error was ignored, and its filters were a hand-kept copy of the page query's. | One `filtered()` builder for both; the error is handled. |
| `relationship_controller.go` `UpdateRelationship` | `db.Save` error ignored; a failed save answered 200. | Error handled. |
| `services/reminder_service.go` | After sending mail, each reminder was written back with `Save` from a snapshot loaded before the run, undoing any edit or completion made meanwhile. | Updates `email_sent` and `last_sent` only. |
| `services/birthday_service.go` `DaysUntilBirthday` | Local-midnight difference divided by 24h is one short across a spring daylight-saving change, so "today" emails and `birthday.occurred` fired a day early. | Days are counted in UTC. |
| `services/import_service.go` `ParseVCF` | Malformed entries skipped the `MaxVCFContacts` check, so a file of junk lines built an unbounded preview. | The limit is checked first. |
| `controllers/export_controller.go` | "Related Contact Name" was always empty: `RelatedContact` was never preloaded. | Preloads `Relationships.RelatedContact`. |
| `services/import_session.go` | Two lookups of the existing contact had no `user_id` filter. Not exploitable today (the id comes from a server-built preview). | Scoped by user. |
| `services/monica_import_session.go` | `s.cancel` was read without the session lock in `Delete` and `CleanupExpired`. | Read under the lock. |
| `main.go` | Startup warned "no mails" whenever Resend was off, even with SMTP configured. | Uses `cfg.EmailEnabled()`. |
| `graph_controller.go`, `api_token_controller.go` | Errors bypassed the `AppError` envelope, or dropped the underlying error. | Use `apperrors` with `WithError`. |
| `errors/middleware.go` `LogError` | Every 400 and 404 was logged at error level. | Warn below 500. |
| `activity_controller.go` `UpdateActivity` | `Save` re-saved the participants after the association had been replaced, re-running each contact's hooks. | `Omit(clause.Associations)`. |

### Backend simplifications

- `applyContactInput` in `contact_controller.go` is now the one place that copies
  `ContactInput` onto a `Contact`, for create and update. The contact-field checklist
  in `CLAUDE.md` gets one step shorter (see 2.3).
- `contactIncludes` replaces the map-plus-switch for `?includes=` and the five
  hand-written preloads in `GetContact`.
- `toAdminUserResponse` replaces four copies of the same struct literal.
- The three password handlers use `middleware.GetValidated` instead of a hand-rolled
  type assertion.

### Frontend bugs

| Where | What was wrong | Fix |
|---|---|---|
| `ContactDetailPage.tsx` | The route reuses one component instance, so an inline edit opened on contact A survived navigation to B and Save wrote A's text onto B. | The page is keyed by contact id. |
| `auth.ts` | Read `data.ID` and `data.Username`; `/users/me` returns `id` and `username`. The cached values were always undefined. Two tests mocked the wrong shape. | Fixed, tests corrected. |
| `EditTimelineItemDialog.tsx` | No `isOptionEqualToValue`, so attached contacts looked unselected and could be added twice. | Compare by `ID`. |
| `AddNoteDialog.tsx`, `AddActivityDialog.tsx` | The default date was the UTC day: yesterday or tomorrow near local midnight. | New `utils/today.ts` `todayISO()`. |
| `NetworkPage.tsx` | The graph was not reloaded after editing or deleting an activity. | Calls `refetch`. |
| `ReminderList.tsx`, `EditableArrayField.tsx` | `try/finally` around callbacks that rethrow gave an unhandled rejection on every failed save. | Added `catch`. |
| `MonicaImportDialog.tsx` | The duplicate tooltip showed the raw `match_reason` (`vcard_uid`). | Translated like the CSV dialog. |
| `SettingsPage.tsx` | The language select was blank for a region tag such as `de-DE`. | Uses `i18n.resolvedLanguage`. |

### Frontend dead code removed

`components/index.ts`, `api/index.ts`, `hooks/index.ts`, `utils/index.ts` (barrels
nothing imported), `hooks/useErrorBoundary.tsx` and `components/ErrorFallback.tsx`
(reachable only through that barrel), `App.css` (create-react-app leftovers).

---

## Part 2: Larger findings

### 2.1 Data loss and security (do these first)

**A. Three contact-deletion paths skip the cascade (reported).**
`controllers/contact_controller.go` deletes a contact's reminders, notes,
relationships, employment history, activity links and photo. These do not:
`carddav/backend.go:384` (delete from a phone), `services/contact_sync_service.go:1108`
(remote deletion) and `services/contact_merge_service.go:316`.
A contact deleted from a phone keeps its reminders, which keep being emailed.
The birthday join at `services/birthday_service.go:64` also lacks
`contacts.deleted_at IS NULL`.
Fix: one `services.DeleteContactCascade(tx, userID, contactID)` used by all four paths.

**B. CardDAV edits never clear a field (reported).**
`carddav/vcard_mapper.go:264-443` `VCardToContact` only overwrites a field when the
property is present. A birthday or phone number removed on the phone stays in Meerkat
and is served back; with two-way sync it is pushed back to the server.
Fix: when updating an existing contact, reset every vCard-mapped field before applying
the card. Related: `GENDER` is imported but never exported (`:392` vs `:34-226`), so a
two-way sync strips it from the remote.

**C. Every edit PUTs the whole cached contact (verified).**
`ContactDetailPage.tsx` sends `{...contact, [field]: value}` and the backend replaces
every column. A change made in another tab or by a CardDAV sync since page load is
silently reverted by an unrelated edit. This is the mechanism behind `348a46f` and the
reason the two column allowlists are load-bearing.
Fix: a `PATCH` that carries only changed fields, or an `updated_at` precondition
answered with 409. Either one retires the allowlist trap.

**D. OIDC email linking can re-link an already linked account (reported).**
`services/oidc_service.go:187-195` overwrites `OIDCSubject` when the email matches,
even if the account is linked to a different subject. Link only when
`user.OIDCSubject == nil`.

**E. Image decoding has no dimension limit (verified in `photo_controller.go`, reported in `vcard_mapper.go`).**
A small PNG that declares 50000×50000 pixels allocates gigabytes. Uploads, vCard
photos and remote address books all decode without `image.DecodeConfig`.
There are two separate photo pipelines (`controllers/photo_controller.go` and
`carddav.SaveContactPhoto`) with different format support. Fix both at once: one photo
package with a pixel cap, used by upload, CardDAV, sync and import. It can also
replace the per-pixel `cropToSquare` loop with `draw.Draw` and move off `nfnt/resize`,
which is archived.

**F. CORS allows any origin with credentials by default (verified).**
`FRONTEND_URL` defaults to `*`, and `main.go` then reflects every origin with
`AllowCredentials: true`. `SameSite=Lax` stops cross-site cookies on writes in current
browsers, so this is a weak default rather than an open hole. Fail startup, or at
least warn, when `FRONTEND_URL=*` and `GIN_MODE=release`. Same check for OIDC: with
`*` the redirect URL becomes `*/api/v1/auth/oidc/callback` and login cannot work.

**G. Deleting data is soft, and never purged (verified).**
Contacts, notes, activities and reminders are soft-deleted and stay in the database
for good. Admin `DeleteUser` hard-deletes the user row but soft-deletes their
contacts and notes, and leaves photo files on disk. For a privacy-focused product,
decide on a policy: hard delete, or a purge job after a grace period.

**H. Smaller items in this group (reported).**
- `carddav/backend.go:250`, `contact_sync_service.go:971`: the unique index on
  `(user_id, vcard_uid)` includes soft-deleted rows but the lookups do not, so
  re-creating a deleted UID fails with 500 on every retry and leaves remote sync
  stuck on "partial".
- `services/import_service.go:991`: merging an imported vCard overwrites the existing
  contact's `VCardUID`, so synced devices see the contact vanish and reappear.
- `services/import_session.go:463-502`: a VCF confirm is not claimed before its photo
  loop. A request that outlives the 30s client timeout can be confirmed twice and
  import every row twice.
- `controllers/carddav_controller.go`: saving or deleting the connection during a
  running sync leaves orphan links and writes the old sync token back.
- `services/mailer.go`: no timeout on SMTP dial or send; a blackholed host hangs the
  reminder job.
- `database/migrate.go`: a dirty migration state is forced clean automatically, which
  skips the migration that failed halfway.

### 2.2 Reminders and dates

- **Repeated webhooks and birthday mails (reported).** `reminder.triggered` and
  `birthday.occurred` have no "already fired" state. With email off, or after a
  restart more than an hour after the last run, they fire again
  (`reminder_service.go:290-306`, `main.go:77`). Record "ran for local date X" in
  `JobExecution`.
- **Mixed timezones (reported).** `SendReminders` uses `REMINDER_TIMEZONE`, while
  `NextOccurrenceAfterToday` and `CalculateNextReminderTime` use UTC
  (`reminder_service.go:501-529`). East of UTC a completed weekly reminder can be due
  again the same day.
- **Monthly on the 31st drifts to the 28th for good (reported)**, `addMonths`
  `:469-484`. Needs a stored anchor day.
- **"Overdue" is computed three ways (reported).** `DashboardPage.tsx:119`,
  `ReminderList.tsx:78` and the backend disagree, and both frontend rules are wrong on
  one side of UTC. `remind_at` is midnight UTC of a calendar day: compare date strings
  in one shared helper.
- **Editing a note or activity date (reported, high impact).**
  `hooks/useTimelineEditing.ts:31-87` and `NetworkPage.tsx:125,145` prefill the
  free-text date field with ISO and save with `new Date(text)`. With the default `eu`
  format, `30.04.2025` throws and `01.05.2025` is saved as 5 January.
  `NotesPage.tsx:116-133` shows the correct pattern (`formatDate` + `parseDateInput`).
- **`formatDate` uses UTC getters for real instants (reported)**: `completed_at` and
  `created_at` can show tomorrow's date in the Americas (`DateFormatProvider.tsx:160`).
- **Calendar sync reads all-day and floating events as UTC (reported)**,
  `calendar_sync_service.go:407,436`.
- **Date format is not applied after login (reported).** `LoginPage.tsx:59` only
  writes localStorage; the mounted provider never re-reads it. After OIDC login the
  saved language and date format are dropped entirely.

### 2.3 Backend structure

**One "find the caller's row" helper (verified).** The block "load by id scoped to
user, else 404, else 500" appears 51 times in `controllers/`, and
`c.MustGet("db").(*gorm.DB)` 94 times. A generic
`findOwned[T](c, db, userID, id, "Contact") (*T, bool)` removes roughly 300 lines and
makes the injection in part 1 impossible to reintroduce. `findOwnedContact` in
`employment_history_controller.go` is the starting point.

**One error envelope (verified).** Three shapes are in use:
`{"error": {"code", "message"}}` from `apperrors`, `{"error": "text"}` from
`middleware/auth.go` (12 sites), `admin.go` and the rate limiter, and a third for
login lockout. `errors.ErrTokenExpired`, `ErrTokenInvalid` and `ErrRateLimitExceeded`
were written for this and have no callers. Use them, then delete what stays unused:
`ErrBusinessLogic`, `AbortWithCustomError`, `IsAppError`, `NewIPRateLimiterWithTTL`,
`JSONBodySizeLimitMiddleware`, `SanitizeString`.

**One way to read configuration (verified).** Handlers get it three ways: a `cfg`
closure in `routes.go`, `currentConfig(c)` from the context, and `os.Getenv`
(`DEMO_MODE` in four places, `PROFILE_PHOTO_DIR` in `deleteContactPhotos`, the rate
limiter bursts). Put `DemoMode` in `Config` and pick one access path. `config.Validate`
also has unreachable branches: `UseResend` and `UseSMTP` are only true when their
fields are already non-empty. A half-configured mailer is silently off; warn instead.

**Reminder is its own DTO (verified).** `models.Reminder` is bound straight from the
request, and `CreateReminder` then resets six server-owned fields by hand. Every other
resource has an `*Input` type. `UpdateLanguage`, `UpdateDateFormat` and `LoginUser`
also decode the body in the controller, against the project rule.

**Writes that belong in a transaction (verified).** `CreateActivity` (row, then
association), `UpdateActivity` (association, then row), `CompleteReminder`
(completion record, then reminder).

**Birthday validation checks the shape only (verified).** `2020-13-45` passes
`validateBirthday`, and the import and CardDAV paths check no more than that either.
Tightening the API alone would block every edit of a contact that already stores such
a date, because each edit sends the stored birthday back (2.1 C). Validate real dates
in one shared function used by the API, the importers and the vCard mapper, and
repair stored values in a migration.

**Contact field checklist.** `MergeImportedContact` (`import_service.go:908-994`) and
the CSV export (`export_controller.go:93-97`) are hand-kept field lists that the
checklist in `CLAUDE.md` does not mention. Both have already drifted: the merge drops
`is_deceased`, and the export omits every field added after the original set (name
parts, organization, anniversary, deceased, archived, multi-value fields).
`CLAUDE.md` also calls the frontend allowlist `CONTACT_FIELDS`; it is
`CONTACT_DETAIL_COLUMNS`, and create/update mapping is now `applyContactInput`.

**Large files (reported).**
- `services/monica_import_session.go` (1409 lines) mixes session management, fetch,
  confirm and reporting. Split into four files in the same package, and replace the
  6 to 9 positional parameters of the `import*` methods with a run struct. The preview
  re-implements the import's ownership rules and can disagree with it.
- `import_session.go` `Confirm` and `ConfirmVCF` are about 150 near-identical lines.
- `contact_sync_service.go` and `calendar_sync_service.go` duplicate the round
  tripper, the rate-limited job wrapper and the sync loop.
- Three implementations of "resolve, drop private IPs, dial": `services/dav_http.go`,
  `httputil/fetch.go`, `monica/client.go`. They already behave differently.
- `carddav`: `ContextWithUser` passes `db` and `photoDir` through the context although
  the `Backend` already holds them. `ListAddressObjects` builds a full vCard with
  base64 photo for every contact on every poll, even when only ETags are asked for.

**Housekeeping (verified unless noted).**
- `health_controller.go` reports version `0.1.0`; releases are at 1.7. Inject it at build time.
- `webhook_deliveries` rows keep the full payload and are never pruned (reported).
- Expired import sessions are swept only when someone starts a new import (reported).
  Register the cleanup on the existing scheduler.
- CSV and VCF previews run up to three queries per row; `DuplicateIndex` exists for
  this and only the Monica import uses it (reported).
- The request log includes the full query string, which carries search terms.
- `staticcheck` on this machine is built with Go 1.23 and cannot load the module;
  `golangci-lint` works.

### 2.4 Frontend structure

**One API helper (reported).** Two error stacks coexist: nine modules throw
`ApiError` through `parseErrorResponse`, six throw a plain `Error` through
`handleResponse`, so `instanceof ApiError` behaves differently per module.
`if (!response.ok) throw …; return response.json()` is repeated about 60 times.
One `apiJson<T>(path, init)` fixes both. `RegisterPage.tsx`, `useOIDCConfig.ts` and
`auth.ts` call `fetch` directly, against the project rule; `apiFetch` needs a
`skipAuthRedirect` option for the `/users/me` probe. `client.ts:93` also overwrites
any caller `AbortSignal`, which is why no request can be cancelled.

**Stale responses (reported).** `useNotes.ts`, the header search in `App.tsx` and
`UsersPage.tsx` have no guard against out-of-order responses; `useContacts` and
`useActivities` do.

**Contact pickers are capped at 100 (reported).** `useTimelineEditing.ts:49`,
`ActivitiesPage.tsx:148` and `NetworkPage.tsx:118` ask for `limit: 1000`; the backend
clamps to 100. `AddActivityDialog.tsx:65` finds its preselected contact in the first
100 only. Three dialogs each have their own search autocomplete, one of them
race-safe. Extract `useContactSearch` from `MergeContactsDialog.tsx:89-121`.

**Silent failures (reported).** `NotesPage`, `ActivitiesPage`, `NetworkPage` and
`DashboardPage` call `handleError` without a notifier or only `console.error`, so a
failed save shows nothing. `WebhooksSettings` and `CalendarSyncSettings` show the
error behind the open dialog. `CustomFieldsSettings` keeps an optimistic list after a
failed save.

**Translations.**
- `ErrorBoundary.tsx:157-202` is English only, and it is the root boundary.
- `api/import.ts:112` `CONTACT_FIELD_LABELS` renders English in the CSV mapping step.
- `err instanceof Error ? err.message : t(...)` appears about 35 times and makes the
  translated fallback unreachable, because the API layer always throws an English
  `Error`. Map `ApiError.code` to i18n keys in one place.
- Backend validation messages are English and shown as they are.
- `prefer_not_to_say` is a valid gender in the backend with no locale key or UI option.

**Shared components that exist nine times (reported).**
- Confirm dialog: `window.confirm` in nine places and five hand-built MUI dialogs.
- Settings card frame: six copies with inconsistent loading and error display.
- Contact display name: eight variants; the template-string ones leave a trailing space.
- `MonicaImportDialog` and `ImportContactsDialog` share seven near-verbatim blocks and
  have already drifted.
- `NotesPage` and `ActivitiesPage` are the same page twice.
- Hover-revealed action buttons in seven files never appear on keyboard focus.

**Large files (reported).**
- `ContactDetailPage.tsx` (961 lines): the same catch block ten times, seven
  `updateContact({...contact, X})` call sites. Extract `useContactDetail(id)` with one
  `saveContact(patch)`; this is also where finding 2.1 C lands.
- `DateFormatProvider.tsx` (660 lines): move the pure functions to
  `utils/dateFormat.ts`. There are three age calculations with two different meanings
  under the same "years old" label, and `parseBirthdayInputWithFormat` accepts
  `31.02.1990`.
- `App.tsx` (636 lines): eleven identical `Suspense` wrappers, plus the header search
  and sidebar.

**Dead code left in place (reported, needs a careful pass).**
- `types/api.ts`, `types/forms.ts`, `types/utils.ts` and most of `types/index.ts`:
  only three user types are imported anywhere, and several definitions contradict the
  real ones in `api/*.ts`.
- `LoadingSkeletons.tsx`: 13 of 20 exports unused.
- Unused API functions: `getNote`, `getAllReminders`, `getReminder`, `getUserById`;
  also `saveToken`, `createErrorHandler`, `getBirthdayFormatHint`.
- `reportWebVitals.ts` is called with no handler, so `web-vitals` can go.
  The `/reminders` route is a placeholder nothing links to.
- Hook contract: `useContacts`, `useNotes` and `useActivities` return named arrays
  instead of `data`, against the project rule.

**Other reported bugs, each small.**
- `ProfilePictureUploadDialog.tsx:220`: the text offers drag and drop, but dropping a
  file navigates the browser away from the app.
- `navigator.clipboard` is undefined on plain-HTTP installs: copying a webhook secret
  or API token throws while the UI says "copied".
- `MonicaImportDialog.tsx:153`: closing during connect leaves a server session that
  holds the Monica API token.
- `ContactsPage.tsx:102`: changing sort or the archived toggle does not reset the page.
- `AppDialog` blocks backdrop clicks but not Esc during a save.
- `NetworkGraph.tsx:69`: a window `mousemove` listener re-renders the whole graph on
  every mouse move.

---

## Suggested order

1. Merge and release part 1 (the injection fix).
2. 2.1 A, B and D: each is a contained change with a clear test.
3. 2.2 timeline date editing, then one shared "overdue" helper.
4. `findOwned[T]` and the single error envelope: mechanical, and they shrink every
   later backend change.
5. 2.1 C (PATCH or precondition) together with `useContactDetail`.
6. The shared frontend pieces: `apiJson`, `ConfirmDialog`, `useContactSearch`.
