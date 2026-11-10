-- Your own traits can match by conditions, the way a Sonarr or Radarr custom
-- format does (release title, resolution, language, source, release group,
-- size, each with negate and required). The match kind's check widens, so
-- the table is made again; ids stay, so Pick rules keep their "own:<id>" keys.
CREATE TABLE own_traits_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT, -- a removed trait's id is never given again
  created_at TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(created_by)),
  updated_at TEXT NOT NULL,
  updated_by TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(updated_by)),
  name TEXT NOT NULL,
  match TEXT NOT NULL CHECK(match IN ('name', 'group', 'conditions')),
  terms TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(terms)),
  conditions TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(conditions))
);
INSERT INTO own_traits_new (id, created_at, created_by, updated_at, updated_by, name, match, terms)
SELECT id, created_at, created_by, updated_at, updated_by, name, match, terms FROM own_traits;
-- Keep the id counter where it was, so a removed trait's id stays unused.
UPDATE sqlite_sequence SET seq = (SELECT max(seq) FROM sqlite_sequence WHERE name IN ('own_traits', 'own_traits_new'))
WHERE name = 'own_traits_new';
INSERT INTO sqlite_sequence (name, seq)
SELECT 'own_traits_new', seq FROM sqlite_sequence
WHERE name = 'own_traits' AND NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'own_traits_new');
DROP TABLE own_traits;
ALTER TABLE own_traits_new RENAME TO own_traits;
CREATE UNIQUE INDEX own_traits_name ON own_traits(lower(name));
