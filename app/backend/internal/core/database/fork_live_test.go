package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"
)

// homelab1019Migrations are the fork's 9001 and 9002 as fork builds up to
// 1.0.19 shipped them, before 1.1.4 made them steps that change nothing.
//
//go:embed testdata/homelab_1019/*.sql
var homelab1019Migrations embed.FS

// liveForkMigrations is the migration set of a fork build 1.0.19, as the
// live database ran it: upstream's 0001-0106 and the fork's 9001-9004.
func liveForkMigrations(t *testing.T) fs.FS {
	t.Helper()
	out := upTo(t, 106).(fstest.MapFS)
	for _, m := range mustEmbedded(t) {
		if m.Version == 9003 || m.Version == 9004 {
			out[m.Name] = &fstest.MapFile{Data: []byte(m.SQL)}
		}
	}
	for _, n := range []string{"9001_download_handling.sql", "9002_site_sources.sql"} {
		b, err := fs.ReadFile(homelab1019Migrations, "testdata/homelab_1019/"+n)
		if err != nil {
			t.Fatal(err)
		}
		out[n] = &fstest.MapFile{Data: b}
	}
	return out
}

// LiveForkRows are rows like the live database's: its qBittorrent with the
// fork's download handling and seeding clean-up, its four site Sources, its
// traits that match by conditions, its Pick rules moved from Sonarr and
// Radarr, its 4K request settings and their undo record, a main copy the
// request app move labelled 4K, and its subtitle sites.
var LiveForkRows = []string{
	`INSERT INTO downloaders(id,created_at,created_by,updated_at,updated_by,name,adapter,protocol,url,category,after_import,seed_goal,post_import_category,own_categories_only)
	 VALUES(1,'a','{}','b','{}','qBittorrent','qbittorrent','torrent','http://qbit:8080','holyarrs','leave','{"ratio":2,"minutes":23040}','holyarrs-imported',1)`,
	`INSERT INTO downloaders(id,created_at,created_by,updated_at,updated_by,name,adapter,protocol,url,category,after_import,seed_goal)
	 VALUES(2,'a','{}','b','{}','Transmission','transmission','torrent','http://tr:9091','holyarrs','leave','{"ratio":1.5}')`,
	`INSERT INTO downloaders(id,created_at,created_by,updated_at,updated_by,name,adapter,protocol,url,category,after_import,seed_goal,post_import_category)
	 VALUES(3,'a','{}','b','{}','qBit big goal','qbittorrent','torrent','http://qbit2:8080','x','leave','{"ratio":5000,"minutes":9000000}','')`,
	`INSERT INTO downloads(id,created_at,created_by,updated_at,updated_by,downloader_id,external_id,progress,state,started_by,imported_at)
	 VALUES(10,'a','{}','b','{}',1,'aaa111',1,'finished','{}','2026-11-01T10:00:00Z')`,
	`INSERT INTO downloads(id,created_at,created_by,updated_at,updated_by,downloader_id,external_id,progress,state,started_by)
	 VALUES(11,'a','{}','b','{}',1,'bbb222',0.5,'downloading','{}')`,
	`INSERT INTO sources(id,created_at,created_by,updated_at,updated_by,name,protocol,format,url,secret_ref,user_agent,freeleech_only,state,extra,limits)
	 VALUES(1,'a','{}','b','{}','IPTorrents','torrent','iptorrents','https://iptorrents.com',7,'Mozilla/5.0 (X11; Linux x86_64) Live',0,'enabled','{}','{}')`,
	`INSERT INTO sources(id,created_at,created_by,updated_at,updated_by,name,protocol,format,url,state,extra,limits) VALUES
	 (2,'a','{}','b','{}','Knaben','torrent','knaben','https://api.knaben.org','enabled','{}','{}'),
	 (3,'a','{}','b','{}','SubsPlease','torrent','subsplease','https://subsplease.org','enabled','{}','{}'),
	 (4,'a','{}','b','{}','TorrentsCSV','torrent','torrentscsv','https://torrents-csv.com','enabled','{}','{}'),
	 (5,'a','{}','b','{}','Indexer','torrent','torznab','https://idx.example/api','enabled','{"k":"v"}','{"gap_ms":500}')`,
	`INSERT INTO own_traits (id, created_at, created_by, updated_at, updated_by, name, match, terms, conditions) VALUES
	 (3, 'c', '{}', 'u', '{}', 'NO: Nordic multi release', 'conditions', '[]', '[{"name":"NORDiC","kind":"release_title","value":"\\b(nordi[ck])\\b","required":true}]'),
	 (5, 'c', '{}', 'u', '{}', 'Not English', 'conditions', '[]', '[{"name":"English","kind":"language","value":"en","except":true,"required":true},{"name":"2160p","kind":"resolution","value":"2160","negate":true}]')`,
	`INSERT INTO pick_rules (id, created_at, created_by, updated_at, updated_by, name, kinds, revision, preset, body) VALUES
	 (8, 'a', '{}', 'b', '{}', 'Nordic Multi-Audio', '["movie"]', 1, 0, '{"steps":[{"quality":"workprint","allowed":false},{"quality":"1080p_web","allowed":true},{"quality":"1080p_bluray","allowed":true},{"quality":"br_disk","allowed":true}],"good_enough_at":"1080p_bluray","traits":[{"trait":"own:3","points":10000}],"must_have":[],"never":[],"min_points":0,"size_limits":{},"protocol":{"prefer":"either"},"language":{"wanted":["no"],"original_ok":false,"mode":"wanted"},"order":"quality_then_points","upgrade_min_points":1,"proper_policy":{"mode":"take_any"},"like_old_apps":true}'),
	 (12, 'a', '{}', 'b', '{}', 'HD-1080p (Sonarr)', '["show"]', 1, 0, '{"steps":[{"quality":"sd_web","allowed":true},{"quality":"720p_tv","allowed":true},{"quality":"1080p_web","allowed":true}],"good_enough_at":"1080p_web","traits":[{"trait":"own:5","points":-100}],"must_have":[],"never":[],"min_points":0,"size_limits":{},"protocol":{"prefer":"either"},"language":{"wanted":[],"original_ok":true,"mode":"any"},"order":"quality_then_points","upgrade_min_points":1,"proper_policy":{"mode":"take_any"},"like_old_apps":true,"no_upgrades":true}')`,
	`INSERT INTO settings (grp, key, value, created_at, created_by, updated_at, updated_by) VALUES
	 ('this_app', 'ask_4k_library_folder_id', '6', 'a', '{}', 'b', '{}'),
	 ('this_app', 'ask_4k_pick_rules_id', '19', 'a', '{}', 'b', '{}'),
	 ('this_app', 'ask_show_4k_library_folder_id', '4', 'a', '{}', 'b', '{}'),
	 ('this_app', 'ask_show_4k_pick_rules_id', '12', 'a', '{}', 'b', '{}')`,
	`INSERT INTO moves (id, created_at, owner_id, state) VALUES (3, 'a', 1, 'done')`,
	`INSERT INTO move_made (move_id, key, what, row_id, before, after, created_at) VALUES
	 (3, 'ask_settings:seerr', 'ask_settings', 0, '{"ask_4k_library_folder_id":{"exists":false}}', '{"ask_4k_library_folder_id":{"exists":true,"value":"6"}}', 'a'),
	 (3, 'trait:radarr:NORDiC', 'trait', 3, '', '', 'a')`,
	`INSERT INTO library_folders (id, created_at, created_by, updated_at, updated_by, path, kind, label) VALUES
	 (6, 'a', '{}', 'b', '{}', '/movies-4k', 'movie', 'Movies 4K')`,
	`INSERT INTO titles (id, created_at, created_by, updated_at, updated_by, kind, name, sort_name, year, metadata_source, why_added) VALUES
	 (1, 'a', '{}', 'b', '{}', 'movie', 'Arrival', 'arrival', 2016, 'tmdb', 'import'),
	 (2, 'a', '{}', 'b', '{}', 'movie', 'Heat', 'heat', 1995, 'tmdb', 'import')`,
	`INSERT INTO versions (id, created_at, created_by, updated_at, updated_by, title_id, label, pick_rules_id, library_folder_id, folder_name, is_main) VALUES
	 (1, 'a', '{}', 'b', '{}', 1, '4K', 8, 6, 'Arrival (2016)', 1),
	 (2, 'a', '{}', 'b', '{}', 2, 'Main', 8, 6, 'Heat (1995)', 1)`,
	`INSERT INTO subtitle_sites (id, created_at, created_by, updated_at, updated_by, name, kind, url) VALUES
	 (1, 'a', '{}', 'b', '{}', 'TVsubtitles', 'tvsubtitles', 'https://www.tvsubtitles.net'),
	 (2, 'a', '{}', 'b', '{}', 'YIFY', 'yify', 'https://yifysubtitles.ch'),
	 (3, 'a', '{}', 'b', '{}', 'Subf2m', 'subf2m', 'https://subf2m.co'),
	 (4, 'a', '{}', 'b', '{}', 'BSPlayer', 'bsplayer', 'http://s1.api.bsplayer-subtitles.com'),
	 (5, 'a', '{}', 'b', '{}', 'OpenSubtitles', 'opensubtitles', 'https://api.opensubtitles.com/api/v1')`,
}

// liveForkFixture writes LiveForkRows.
func liveForkFixture(t *testing.T, s *Store) {
	t.Helper()
	if err := s.Write(context.Background(), func(tx *sql.Tx) error {
		for _, q := range LiveForkRows {
			if _, err := tx.Exec(q); err != nil {
				return fmt.Errorf("%s: %w", q, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// oneValue reads one value as text.
func oneValue(t *testing.T, db *sql.DB, q string, args ...any) string {
	t.Helper()
	var v sql.NullString
	if err := db.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v.String
}

// checkLiveForkValues checks every live value sits in upstream's columns
// and keys after the upgrade.
func checkLiveForkValues(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, c := range []struct{ q, want string }{
		// Downloaders: the after-import category, own items only and the
		// seeding clean-up as upstream's remove after seeding.
		{"SELECT category_after_import || '|' || own_only || '|' || after_import || '|' || seed_goal FROM downloaders WHERE id = 1",
			`holyarrs-imported|1|remove|{"ratio":2,"minutes":23040}`},
		{"SELECT category_after_import || '|' || own_only || '|' || after_import FROM downloaders WHERE id = 2", "|0|leave"},
		{"SELECT after_import || '|' || seed_goal FROM downloaders WHERE id = 3", `remove|{"ratio":1000,"minutes":525600}`},
		{"SELECT count(*) FROM pragma_table_info('downloaders') WHERE name IN ('post_import_category', 'own_categories_only')", "0"},
		{"SELECT count(*) FROM sqlite_master WHERE name = 'fork_downloaders_kept'", "0"},
		// Downloads: when the import moved it.
		{"SELECT ifnull(category_moved_at, 'null') FROM downloads WHERE id = 10", "2026-11-01T10:00:00Z"},
		{"SELECT ifnull(category_moved_at, 'null') FROM downloads WHERE id = 11", "null"},
		{"SELECT count(*) FROM pragma_table_info('downloads') WHERE name = 'imported_at'", "0"},
		// Sources: the built-in site form, with upstream's cookie sign-in and
		// user agent (9006), the freeleech choice and polite limits in their
		// keys; the secret stays.
		{"SELECT format || '|' || json_extract(extra, '$.site') || '|' || json_extract(extra, '$.site_login') || '|' || json_extract(extra, '$.site_agent') || '|' || json_extract(extra, '$.freeleech_only') || '|' || secret_ref || '|' || state FROM sources WHERE id = 1",
			"site|iptorrents|cookie|Mozilla/5.0 (X11; Linux x86_64) Live||7|enabled"},
		{"SELECT count(*) FROM sources WHERE json_type(extra, '$.user_agent') IS NOT NULL", "0"},
		{"SELECT json_type(extra, '$.freeleech_only') FROM sources WHERE id = 1", "text"},
		{"SELECT limits FROM sources WHERE id = 1", `{"gap_ms":3000,"searches_per_window":60,"window_seconds":3600,"timeout_ms":30000}`},
		{"SELECT group_concat(format || ':' || json_extract(extra, '$.site'), ',') FROM (SELECT * FROM sources WHERE id IN (2, 3, 4) ORDER BY id)",
			"site:knaben,site:subsplease,site:torrentscsv"},
		{"SELECT limits FROM sources WHERE id = 2", `{"gap_ms":2000,"searches_per_window":100,"window_seconds":3600,"timeout_ms":20000}`},
		{"SELECT format || '|' || extra || '|' || limits FROM sources WHERE id = 5", `torznab|{"k":"v"}|{"gap_ms":500}`},
		{"SELECT count(*) FROM pragma_table_info('sources') WHERE name IN ('user_agent', 'freeleech_only')", "0"},
		// The fork brought extra files along by default: side files on.
		{"SELECT group_concat(key || '=' || value, ',') FROM (SELECT * FROM settings WHERE grp = 'where_files_go' AND key LIKE 'side_file%' ORDER BY key)",
			"side_file_types=srt,sub,idx,ass,ssa,side_files=true"},
		// Request settings under upstream's names, and in their undo record.
		{"SELECT group_concat(key || '=' || value, ',') FROM (SELECT * FROM settings WHERE grp = 'this_app' AND key LIKE 'ask_%four_k%' ORDER BY key)",
			"ask_four_k_library_folder_id=6,ask_four_k_pick_rules_id=19,ask_show_four_k_library_folder_id=4,ask_show_four_k_pick_rules_id=12"},
		{"SELECT count(*) FROM settings WHERE key LIKE '%\\_4k\\_%' ESCAPE '\\'", "0"},
		{"SELECT before || after FROM move_made WHERE what = 'ask_settings'",
			`{"ask_four_k_library_folder_id":{"exists":false}}{"ask_four_k_library_folder_id":{"exists":true,"value":"6"}}`},
		{"SELECT what FROM move_made WHERE key = 'trait:radarr:NORDiC'", "own_trait"},
		// The 4K folder's main copy is the title's 4K copy by its mark.
		{"SELECT group_concat(id || ':' || four_k, ',') FROM (SELECT * FROM versions ORDER BY id)", "1:1,2:0"},
		// Subtitle sites by upstream's kinds, all still on.
		{"SELECT group_concat(kind || ':' || state, ',') FROM (SELECT * FROM subtitle_sites ORDER BY id)",
			"tvsubtitles:enabled,yifysubtitles:enabled,subf2m:enabled,bsplayer:enabled,opensubtitles:enabled"},
		// Traits and Pick rules keep their rows and ids; their conversion is
		// a start-up step (library.UpgradeForkRules).
		{"SELECT group_concat(id || ':' || match, ',') FROM (SELECT * FROM own_traits ORDER BY id)", "3:conditions,5:conditions"},
		{"SELECT json_extract(body, '$.like_old_apps') FROM pick_rules WHERE id = 8", "1"},
	} {
		if got := oneValue(t, db, c.q); got != c.want {
			t.Errorf("%s\n got  %s\n want %s", c.q, got, c.want)
		}
	}
}
