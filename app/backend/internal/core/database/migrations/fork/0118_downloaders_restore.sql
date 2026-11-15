-- After upstream's 0118 on a database a fork build updated: the Downloader
-- columns set aside before 0109 (0097_downloaders_keep.sql) go into
-- upstream's category_after_import and own_only.
UPDATE downloaders SET
  category_after_import = coalesce((SELECT k.post_import_category FROM fork_downloaders_kept k WHERE k.id = downloaders.id), ''),
  own_only = coalesce((SELECT k.own_categories_only FROM fork_downloaders_kept k WHERE k.id = downloaders.id), 0)
  WHERE id IN (SELECT id FROM fork_downloaders_kept);
DROP TABLE fork_downloaders_kept;
