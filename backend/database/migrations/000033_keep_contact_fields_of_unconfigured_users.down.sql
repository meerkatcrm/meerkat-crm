-- A selection someone has changed since is theirs to keep; only the untouched value goes back to NULL.
UPDATE users
SET enabled_contact_fields = NULL
WHERE enabled_contact_fields = '["emails","phones","addresses","nickname","gender","birthday","how_we_met","food_preference","work_information","contact_information"]';
