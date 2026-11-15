-- Fork builds before 1.1.4 added download handling here: the Downloader
-- columns post_import_category and own_categories_only, and
-- downloads.imported_at. Upstream 1.1.4 has its own (0118:
-- category_after_import, own_only, category_moved_at), so this step adds
-- nothing now. A database that ran the old step has its values carried into
-- upstream's columns by the runner's fork hooks around 0109 and 0118
-- (fork_hooks.go). The file stays, under its name, because the runner
-- refuses a recorded migration it does not know.
SELECT 1;
