-- The legacy photo_thumbnail column was never cleared by the export, so thumbnails exported to
-- files before the rollback are still in the database. Thumbnails of photos added after the
-- upgrade exist only as files and are missing after a downgrade until the photo is uploaded again.
ALTER TABLE contacts DROP COLUMN photo_thumbnail_file;
