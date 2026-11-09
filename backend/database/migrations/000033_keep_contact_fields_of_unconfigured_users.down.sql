-- Rows still holding the previous defaults go back to NULL, including anyone who chose exactly that
-- list themselves. Their view does not change: before this migration, NULL meant that same list.
UPDATE users
SET enabled_contact_fields = NULL
WHERE enabled_contact_fields = '["emails","phones","addresses","nickname","gender","birthday","how_we_met","food_preference","work_information","contact_information"]';
