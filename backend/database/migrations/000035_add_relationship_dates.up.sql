-- Optional start and end of a relationship (e.g. a marriage that began and later ended).
-- Partial dates: YYYY, YYYY-MM or YYYY-MM-DD. Empty means unknown / still ongoing.
ALTER TABLE relationships ADD COLUMN start_date TEXT DEFAULT '';
ALTER TABLE relationships ADD COLUMN end_date TEXT DEFAULT '';
