-- Opt-in public access through a gate container: the published host port (0 = off)
-- and the allowed client addresses (comma-separated IPs/CIDRs; empty = anyone).
-- The gate's TLS certificate and key are kept in the encrypted credentials.
ALTER TABLE db_instances ADD COLUMN public_port INTEGER NOT NULL DEFAULT 0 CHECK(public_port BETWEEN 0 AND 65535);
ALTER TABLE db_instances ADD COLUMN public_allow TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX db_instances_public_port ON db_instances(public_port) WHERE public_port > 0;
