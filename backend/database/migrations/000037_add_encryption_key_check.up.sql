-- A fixed text encrypted with ENCRYPTION_KEY: a wrong key is found at startup.
CREATE TABLE encryption_key_check (
    id    INTEGER PRIMARY KEY CHECK (id = 1),
    value TEXT    NOT NULL
);
