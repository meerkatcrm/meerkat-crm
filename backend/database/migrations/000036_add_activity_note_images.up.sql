-- One optional image per activity and per note: the filename of a JPEG in the
-- profile photo directory. Empty means no image.
ALTER TABLE activities ADD COLUMN image TEXT NOT NULL DEFAULT '';
ALTER TABLE notes ADD COLUMN image TEXT NOT NULL DEFAULT '';
