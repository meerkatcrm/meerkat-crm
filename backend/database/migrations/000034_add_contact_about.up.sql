-- "About" is free text about a contact, mapped both ways to the vCard NOTE property.
-- NOTE used to be unmapped, so values that arrived over CardDAV or a VCF import sit in
-- the hidden vcard_extra blob. Move them into the new column and strip them from
-- vcard_extra: ContactToVCard skips mapped properties found there, so a NOTE left
-- behind would no longer be exported and the next sync would delete it on the phone.
--
-- Several NOTE properties (vCard allows it, clients rarely write it) are joined with a
-- blank line, the same as VCardToContact does. Values are capped at the column's
-- 5000-character limit, or every later edit of the contact would fail validation.
ALTER TABLE contacts ADD COLUMN about TEXT DEFAULT '' NOT NULL;

UPDATE contacts
SET about = substr((
  SELECT group_concat(note, char(10) || char(10))
  FROM (
    SELECT trim(json_extract(value, '$.Value')) AS note
    FROM json_each(vcard_extra, '$.properties.NOTE')
    WHERE trim(COALESCE(json_extract(value, '$.Value'), '')) != ''
    ORDER BY key
  )
), 1, 5000)
WHERE vcard_extra IS NOT NULL AND vcard_extra != '' AND json_valid(vcard_extra)
  AND json_type(vcard_extra, '$.properties.NOTE') = 'array';

UPDATE contacts SET about = '' WHERE about IS NULL;

UPDATE contacts
SET vcard_extra = json_remove(vcard_extra, '$.properties.NOTE')
WHERE vcard_extra IS NOT NULL AND vcard_extra != '' AND json_valid(vcard_extra)
  AND json_type(vcard_extra, '$.properties.NOTE') IS NOT NULL;

-- Users who saved their own field selection would not see the new field, so a note
-- that came from their phone would still look lost. Switch it on for those who now
-- have one. NULL selections get the frontend defaults, which include About.
UPDATE users
SET enabled_contact_fields = json_insert(enabled_contact_fields, '$[#]', 'about')
WHERE enabled_contact_fields IS NOT NULL AND json_valid(enabled_contact_fields)
  AND json_type(enabled_contact_fields) = 'array'
  AND NOT EXISTS (SELECT 1 FROM json_each(users.enabled_contact_fields) WHERE value = 'about')
  AND EXISTS (
    SELECT 1 FROM contacts
    WHERE contacts.user_id = users.id
      AND contacts.deleted_at IS NULL
      AND contacts.about != ''
  );
