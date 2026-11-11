-- contacts.note is free text about a contact, mapped both ways to the vCard NOTE property.
-- NOTE used to be unmapped, so values that arrived over CardDAV or a VCF import sit in
-- the hidden vcard_extra blob. Move them into the new column and strip them from
-- vcard_extra: ContactToVCard skips mapped properties found there, so a NOTE left
-- behind would no longer be exported and the next sync would delete it on the phone.
--
-- Several NOTE properties (vCard allows it, clients rarely write it) are joined with a
-- blank line, the same as VCardToContact does. Values are capped at the column's
-- 5000-character limit, or every later edit of the contact would fail validation. A
-- card whose NOTEs are all blank yields NULL from group_concat, hence the COALESCE.
-- vcard_extra holds the value as go-vcard decoded it, which still contains the "\;"
-- escape; it is undone here exactly as noteFromCard does for new cards.
ALTER TABLE contacts ADD COLUMN note TEXT DEFAULT '' NOT NULL;

UPDATE contacts
SET note = COALESCE(substr((
  SELECT group_concat(value_text, char(10) || char(10))
  FROM (
    SELECT trim(replace(json_extract(value, '$.Value'), '\;', ';')) AS value_text
    FROM json_each(vcard_extra, '$.properties.NOTE')
    WHERE trim(COALESCE(json_extract(value, '$.Value'), '')) != ''
    ORDER BY key
  )
), 1, 5000), '')
WHERE vcard_extra IS NOT NULL AND vcard_extra != '' AND json_valid(vcard_extra)
  AND json_type(vcard_extra, '$.properties.NOTE') = 'array';

UPDATE contacts
SET vcard_extra = json_remove(vcard_extra, '$.properties.NOTE')
WHERE vcard_extra IS NOT NULL AND vcard_extra != '' AND json_valid(vcard_extra)
  AND json_type(vcard_extra, '$.properties.NOTE') IS NOT NULL;

-- Users who saved their own field selection would not see the new field, so a note
-- that comes from their phone, now or with a later sync, would still look lost (the
-- symptom of #290). Unlike a field users type into, the note is filled by sync, so the
-- field is switched on for every saved selection. An empty selection is a deliberate
-- choice to show no extended fields and is left alone. NULL selections get the
-- frontend defaults, which include it.
UPDATE users
SET enabled_contact_fields = json_insert(enabled_contact_fields, '$[#]', 'note')
WHERE enabled_contact_fields IS NOT NULL AND json_valid(enabled_contact_fields)
  AND json_type(enabled_contact_fields) = 'array'
  AND EXISTS (SELECT 1 FROM json_each(users.enabled_contact_fields))
  AND NOT EXISTS (SELECT 1 FROM json_each(users.enabled_contact_fields) WHERE value = 'note');
