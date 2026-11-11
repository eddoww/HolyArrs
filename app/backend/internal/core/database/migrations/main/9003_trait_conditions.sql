-- Your own traits can match by conditions, the way a Sonarr or Radarr custom
-- format does (release title, resolution, language, source, release group,
-- size, each with negate and required). The match kind's check widens, so
-- the table is made again; ids and the AUTOINCREMENT mark stay, so Pick
-- rules keep their "own:<id>" keys and a removed trait's id is never given
-- again.
CREATE TABLE own_traits_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(created_by)),
  updated_at TEXT NOT NULL,
  updated_by TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(updated_by)),
  name TEXT NOT NULL,
  match TEXT NOT NULL CHECK(match IN ('name', 'group', 'size', 'conditions')),
  terms TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(terms)),
  source TEXT NOT NULL DEFAULT '',
  source_id TEXT NOT NULL DEFAULT '',
  specs TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(specs)),
  conditions TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(conditions))
);
INSERT INTO own_traits_new (id, created_at, created_by, updated_at, updated_by, name, match, terms, source, source_id, specs)
  SELECT id, created_at, created_by, updated_at, updated_by, name, match, terms, source, source_id, specs FROM own_traits;
INSERT INTO sqlite_sequence (name, seq)
  SELECT 'own_traits_new', seq FROM sqlite_sequence WHERE name = 'own_traits'
  AND NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'own_traits_new');
UPDATE sqlite_sequence SET seq = max(seq, (SELECT seq FROM sqlite_sequence WHERE name = 'own_traits'))
  WHERE name = 'own_traits_new' AND EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'own_traits');
DROP TABLE own_traits;
ALTER TABLE own_traits_new RENAME TO own_traits;
CREATE UNIQUE INDEX own_traits_name ON own_traits(lower(name));
CREATE UNIQUE INDEX own_traits_source ON own_traits(source, source_id) WHERE source != '';
