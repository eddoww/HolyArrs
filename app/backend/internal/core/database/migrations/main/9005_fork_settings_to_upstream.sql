-- A database a fork build updated keeps settings under the fork's names
-- where upstream 1.1.4 now has its own. Each statement only renames or marks
-- rows a fork build wrote, so on a new database it changes nothing.

-- The fork's 4K request folders and Pick rules: upstream reads them as
-- ask_four_k_*. A value already under upstream's name gives way to the
-- fork's, which is the one the owner set.
UPDATE OR REPLACE settings SET key = replace(key, '_4k_', '_four_k_')
  WHERE grp = 'this_app' AND key IN ('ask_4k_library_folder_id', 'ask_4k_pick_rules_id',
    'ask_show_4k_library_folder_id', 'ask_show_4k_pick_rules_id');

-- The undo records of the fork's request app move name the same settings.
UPDATE move_made SET before = replace(before, '_4k_', '_four_k_'), after = replace(after, '_4k_', '_four_k_')
  WHERE what = 'ask_settings' AND (before LIKE '%\_4k\_%' ESCAPE '\' OR after LIKE '%\_4k\_%' ESCAPE '\');

-- The fork's move made custom formats into traits marked 'trait'; upstream
-- marks the traits its imports make 'own_trait', and undoes those.
UPDATE move_made SET what = 'own_trait' WHERE what = 'trait';

-- The fork's request app move labelled the main copy in a 4K folder '4K'
-- and had no 4K ask behind it. Upstream knows a 4K copy by its mark (0120),
-- so those copies get it; a title keeps at most one.
UPDATE versions SET four_k = 1 WHERE is_main = 1 AND label = '4K' COLLATE NOCASE
  AND title_id NOT IN (SELECT title_id FROM versions WHERE four_k = 1);

-- The fork's YIFY Subtitles site is upstream's yifysubtitles.
UPDATE subtitle_sites SET kind = 'yifysubtitles' WHERE kind = 'yify';
