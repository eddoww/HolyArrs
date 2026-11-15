-- After upstream's 0118 on a database a fork build updated: the fork's
-- "import extra files" switch and file types become upstream's side files
-- setting (types of 8 letters or digits or fewer, at most 10). The fork
-- brought extra files along by default, so a switch never saved is on,
-- with the fork's default types. A side files setting already saved stays.
INSERT INTO settings (grp, key, value, created_at, created_by, updated_at, updated_by)
  SELECT 'where_files_go', 'side_files',
    coalesce((SELECT value FROM settings WHERE grp = 'where_files_go' AND key = 'import_extra_files'), 'true'),
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), '{"kind":"system"}', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), '{"kind":"system"}'
  WHERE NOT EXISTS (SELECT 1 FROM settings WHERE grp = 'where_files_go' AND key = 'side_files');
INSERT INTO settings (grp, key, value, created_at, created_by, updated_at, updated_by)
  SELECT 'where_files_go', 'side_file_types',
    coalesce(
      (SELECT group_concat(t, ',') FROM (
        SELECT DISTINCT lower(trim(trim(j.value), '.')) AS t FROM settings s,
          json_each('["' || replace(replace(s.value, '"', ''), ',', '","') || '"]') j
        WHERE s.grp = 'where_files_go' AND s.key = 'extra_file_extensions'
          AND length(lower(trim(trim(j.value), '.'))) BETWEEN 1 AND 8
          AND lower(trim(trim(j.value), '.')) NOT GLOB '*[^a-z0-9]*'
        LIMIT 10)),
      CASE WHEN EXISTS (SELECT 1 FROM settings WHERE grp = 'where_files_go' AND key = 'extra_file_extensions')
        THEN 'srt' ELSE 'srt,sub,idx,ass,ssa' END),
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), '{"kind":"system"}', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), '{"kind":"system"}'
  WHERE NOT EXISTS (SELECT 1 FROM settings WHERE grp = 'where_files_go' AND key = 'side_file_types');
