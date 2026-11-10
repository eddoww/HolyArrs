-- The Move tool tells two old apps of the same kind apart (two Sonarrs):
-- a read keeps the address or backup it came from, and the key kept for
-- "Read again" is kept per app and address. Keys kept before this have no
-- address column yet ('') and are read by the address in their secret.
ALTER TABLE migrate_snapshots ADD COLUMN address TEXT NOT NULL DEFAULT '';
CREATE TABLE move_credentials_new (
    kind       TEXT NOT NULL CHECK (kind IN ('radarr', 'sonarr', 'lidarr', 'prowlarr', 'bazarr', 'seerr', 'overseerr', 'qbittorrent', 'sabnzbd')),
    address    TEXT NOT NULL DEFAULT '',
    secret_id  INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL,
    PRIMARY KEY (kind, address)
);
INSERT INTO move_credentials_new (kind, address, secret_id, created_at, created_by)
SELECT kind, '', secret_id, created_at, created_by FROM move_credentials;
DROP TABLE move_credentials;
ALTER TABLE move_credentials_new RENAME TO move_credentials;
