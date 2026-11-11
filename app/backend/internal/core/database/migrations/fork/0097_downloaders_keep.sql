-- A fork database that ran the fork's old 0097_download_handling has the
-- Downloader columns post_import_category and own_categories_only, but not
-- upstream's 0099 (the categories per kind) or 0106 (the rTorrent rebuild,
-- which copies only upstream's columns). Their values are kept here while
-- 0099 and 0106 run as on any other database.
CREATE TABLE fork_downloaders_kept AS
  SELECT id, post_import_category, own_categories_only FROM downloaders;
