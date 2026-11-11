-- holyarrs:foreign-key-rebuild
-- Site Sources: HolyArrs reads IPTorrents, Knaben, SubsPlease and
-- TorrentsCSV itself, without Jackett or Prowlarr in between. Each site is
-- its own format. A site that signs in with a browser cookie keeps the
-- cookie in the vault (secret_ref) and the browser's user agent here, with
-- the choice to list freeleech Releases only. Every row, its id and its
-- Releases and call record stay.

DROP TRIGGER import_created_source_resumed;
DROP TRIGGER import_created_source_gone;

CREATE TABLE sources_new (
    id           INTEGER PRIMARY KEY,
    created_at TEXT NOT NULL, created_by TEXT NOT NULL,
    updated_at TEXT NOT NULL, updated_by TEXT NOT NULL,
    name         TEXT NOT NULL UNIQUE,
    protocol     TEXT NOT NULL CHECK (protocol IN ('torrent', 'usenet')),
    format       TEXT NOT NULL CHECK (format IN ('newznab', 'torznab', 'rss', 'bridge', 'iptorrents', 'knaben', 'subsplease', 'torrentscsv')),
    url          TEXT NOT NULL,
    secret_ref   INTEGER,
    use_for      TEXT NOT NULL DEFAULT '{}',
    kinds        TEXT NOT NULL DEFAULT '[]',
    categories   TEXT NOT NULL DEFAULT '{}',
    min_seeders  INTEGER,
    limits       TEXT NOT NULL DEFAULT '{}',
    proxy_id     INTEGER,
    priority     INTEGER NOT NULL DEFAULT 25,
    state        TEXT NOT NULL DEFAULT 'enabled' CHECK (state IN ('enabled', 'paused', 'disabled')),
    pause_reason TEXT CHECK (pause_reason IN ('down', 'rate_limited', 'login_refused', 'blocked', 'broken_definition')),
    paused_until TEXT,
    extra        TEXT NOT NULL DEFAULT '{}',
    allow_local_network INTEGER NOT NULL DEFAULT 0,
    proxy_url TEXT NOT NULL DEFAULT '',
    proxy_secret_ref INTEGER,
    proxy_allow_local_network INTEGER NOT NULL DEFAULT 0,
    path_secret_ref INTEGER,
    tags TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(tags)),
    user_agent TEXT NOT NULL DEFAULT '',
    freeleech_only INTEGER NOT NULL DEFAULT 0
);

INSERT INTO sources_new (id, created_at, created_by, updated_at, updated_by, name, protocol, format, url, secret_ref,
    use_for, kinds, categories, min_seeders, limits, proxy_id, priority, state, pause_reason, paused_until, extra,
    allow_local_network, proxy_url, proxy_secret_ref, proxy_allow_local_network, path_secret_ref, tags)
SELECT id, created_at, created_by, updated_at, updated_by, name, protocol, format, url, secret_ref,
    use_for, kinds, categories, min_seeders, limits, proxy_id, priority, state, pause_reason, paused_until, extra,
    allow_local_network, proxy_url, proxy_secret_ref, proxy_allow_local_network, path_secret_ref, tags FROM sources;

DROP TABLE sources;
ALTER TABLE sources_new RENAME TO sources;

CREATE TRIGGER import_created_source_resumed AFTER UPDATE OF state ON sources
WHEN OLD.state = 'disabled' AND NEW.state <> 'disabled'
BEGIN
    UPDATE import_created SET live = 0 WHERE tbl = 'sources' AND item_id = NEW.id;
END;

CREATE TRIGGER import_created_source_gone AFTER DELETE ON sources
BEGIN
    UPDATE import_created SET live = 0 WHERE tbl = 'sources' AND item_id = OLD.id;
END;
