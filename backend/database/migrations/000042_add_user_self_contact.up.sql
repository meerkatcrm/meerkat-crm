-- The contact that represents the user ("me"). Optional: NULL means no contact is marked.
-- Kept on the user rather than as a flag on contacts, so at most one contact per user can be
-- "me" by construction. No FOREIGN KEY: contacts are only ever soft-deleted, so the constraint
-- would never fire; DeleteContactCascade and contact merges keep the column consistent instead.
ALTER TABLE users ADD COLUMN self_contact_id INTEGER;
