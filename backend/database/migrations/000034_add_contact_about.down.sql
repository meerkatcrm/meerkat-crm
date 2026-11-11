-- Hand About back to vcard_extra as a NOTE property before the column goes, so the
-- previous release keeps round-tripping it over CardDAV instead of losing it.
UPDATE contacts
SET vcard_extra = json_set(
  CASE
    WHEN vcard_extra IS NULL OR vcard_extra = '' OR NOT json_valid(vcard_extra) THEN '{"properties":{}}'
    WHEN json_type(vcard_extra, '$.properties') = 'object' THEN vcard_extra
    ELSE json_set(vcard_extra, '$.properties', json('{}'))
  END,
  '$.properties.NOTE', json_array(json_object('Value', about)))
WHERE about != '';

UPDATE users
SET enabled_contact_fields = (
  SELECT json_group_array(value) FROM json_each(users.enabled_contact_fields) WHERE value != 'about'
)
WHERE enabled_contact_fields IS NOT NULL AND json_valid(enabled_contact_fields)
  AND EXISTS (SELECT 1 FROM json_each(users.enabled_contact_fields) WHERE value = 'about');

ALTER TABLE contacts DROP COLUMN about;
