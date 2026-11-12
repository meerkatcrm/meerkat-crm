-- Thumbnails move out of the database into files next to the photos. photo_thumbnail_file
-- holds the file name ('' = none). The old photo_thumbnail column (a base64 data URL) is left
-- untouched: services.ExportLegacyThumbnails copies its content into files at startup, and a
-- later migration drops the column once nobody needs to roll back.
ALTER TABLE contacts ADD COLUMN photo_thumbnail_file TEXT NOT NULL DEFAULT '';
