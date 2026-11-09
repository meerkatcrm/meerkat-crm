-- NULL enabled_contact_fields means "use the frontend's defaults", and those defaults now hide
-- Work Information. Users who exist today keep the fields they have been seeing, written out as
-- the previous defaults. Accounts created from here on stay NULL and get the new defaults.
UPDATE users
SET enabled_contact_fields = '["emails","phones","addresses","nickname","gender","birthday","how_we_met","food_preference","work_information","contact_information"]'
WHERE enabled_contact_fields IS NULL;
