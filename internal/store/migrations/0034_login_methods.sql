CREATE TABLE passkeys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rp_id TEXT NOT NULL,
    credential_id TEXT NOT NULL,
    name TEXT NOT NULL,
    credential BLOB NOT NULL,
    created_at TEXT NOT NULL,
    last_used_at TEXT,
    UNIQUE(rp_id, credential_id)
);
CREATE INDEX passkeys_user ON passkeys(user_id);

CREATE TABLE login_identities (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    subject TEXT NOT NULL,
    login TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(user_id, provider),
    UNIQUE(provider, subject)
);
