-- After upstream's 0118 on a database a fork build updated: a download the
-- fork imported was moved to the after-import category at import, which
-- upstream records as category_moved_at. The fork's column then goes, so
-- the schema is upstream's.
UPDATE downloads SET category_moved_at = imported_at WHERE imported_at IS NOT NULL AND category_moved_at IS NULL;
ALTER TABLE downloads DROP COLUMN imported_at;
