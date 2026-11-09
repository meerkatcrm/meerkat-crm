-- NULL enabled_contact_fields means "use the frontend's defaults", and those defaults now hide
-- Work Information. Users with no saved selection who have it on a contact keep the fields they have
-- been seeing, written out as the previous defaults, so their notes do not seem to vanish. Fields
-- added in later releases stay off for them until switched on. Everyone else stays NULL and gets
-- the new defaults.
UPDATE users
SET enabled_contact_fields = '["emails","phones","addresses","nickname","gender","birthday","how_we_met","food_preference","work_information","contact_information"]'
WHERE enabled_contact_fields IS NULL
  AND EXISTS (
    SELECT 1 FROM contacts
    WHERE contacts.user_id = users.id
      AND contacts.deleted_at IS NULL
      AND TRIM(COALESCE(contacts.work_information, '')) != ''
  );
