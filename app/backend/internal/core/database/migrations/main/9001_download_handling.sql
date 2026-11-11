-- Download handling next to other apps on the same client.
-- post_import_category: a torrent HolyArrs imported moves to this
-- category ('' leaves it where it is). own_categories_only: the Downloads
-- screen shows and steers only torrents in the Downloader's own categories.
-- It stays off for Downloaders already here; new and moved ones start with
-- it on. imported_at: when the Download's import fully succeeded, so the
-- seeding clean-up removes only torrents HolyArrs imported.
ALTER TABLE downloaders ADD COLUMN post_import_category TEXT NOT NULL DEFAULT '';
ALTER TABLE downloaders ADD COLUMN own_categories_only INTEGER NOT NULL DEFAULT 0;
ALTER TABLE downloads ADD COLUMN imported_at TEXT;
