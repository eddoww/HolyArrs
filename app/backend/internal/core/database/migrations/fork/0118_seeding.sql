-- After upstream's 0118 on a database a fork build updated: the fork's
-- seeding clean-up ran on a qBittorrent Downloader with a ratio or time
-- goal; upstream does that with remove after seeding, and takes a ratio up
-- to 1000 and a time up to a year.
UPDATE downloaders SET after_import = 'remove' WHERE adapter = 'qbittorrent' AND json_valid(seed_goal)
  AND (json_extract(seed_goal, '$.ratio') IS NOT NULL OR json_extract(seed_goal, '$.minutes') IS NOT NULL);
UPDATE downloaders SET seed_goal = json_set(seed_goal, '$.ratio', 1000)
  WHERE json_valid(seed_goal) AND json_extract(seed_goal, '$.ratio') > 1000;
UPDATE downloaders SET seed_goal = json_set(seed_goal, '$.minutes', 525600)
  WHERE json_valid(seed_goal) AND json_extract(seed_goal, '$.minutes') > 525600;
