-- The kept Downloader columns come back as 9001 adds them on any other
-- database, each row with the values it had.
ALTER TABLE downloaders ADD COLUMN post_import_category TEXT NOT NULL DEFAULT '';
ALTER TABLE downloaders ADD COLUMN own_categories_only INTEGER NOT NULL DEFAULT 0;
UPDATE downloaders SET
  post_import_category = (SELECT k.post_import_category FROM fork_downloaders_kept k WHERE k.id = downloaders.id),
  own_categories_only = (SELECT k.own_categories_only FROM fork_downloaders_kept k WHERE k.id = downloaders.id)
  WHERE id IN (SELECT id FROM fork_downloaders_kept);
DROP TABLE fork_downloaders_kept;
