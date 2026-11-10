package database

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

// 0098 rebuilds sources to take the site formats: every row, its Releases,
// its call record and the import marks stay, and the triggers still fire.
func TestSiteSourcesMigrationKeepsData(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	old, err := Open(dir, Options{MainMigrations: upTo(t, 97)})
	if err != nil {
		t.Fatal(err)
	}
	err = old.Write(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO sources(id,created_at,created_by,updated_at,updated_by,name,protocol,format,url,secret_ref,use_for,kinds,categories,min_seeders,limits,priority,state,pause_reason,paused_until,extra,allow_local_network,proxy_url,proxy_secret_ref,proxy_allow_local_network,path_secret_ref,tags)
			 VALUES(5,'a','{}','b','{}','Prowlarr IPT','torrent','torznab','http://prowlarr.example/4/api',71,'{"feed":true}','["movie","tv"]','{"movie":[2000]}',1,'{"gap_ms":10}',3,'paused','down','later','{"failures":"2"}',1,'socks5://p:1080',72,1,73,'["anime"]')`,
			`INSERT INTO sources(id,created_at,created_by,updated_at,updated_by,name,protocol,format,url,state) VALUES(9,'c','{}','d','{}','Feed','torrent','rss','https://rss.example/feed','disabled')`,
			`INSERT INTO releases(id,created_at,created_by,updated_at,updated_by,source_id,name,link_ref,protocol,found_by,state) VALUES(31,'a','{}','b','{}',5,'Dune.2021.1080p','http://x/1','torrent','feed','seen')`,
			`INSERT INTO source_calls(source_id,at,op,ok,ms,error_kind) VALUES(5,'t','search',1,40,'')`,
			`INSERT INTO import_created(tbl,item_id,live) VALUES('sources',9,1)`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := downloaderRows(t, old.Read(), "SELECT * FROM sources ORDER BY id")
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := downloaderRows(t, s.Read(), "SELECT * FROM sources ORDER BY id")
	for i := range got {
		if len(got[i]) < len(before[0])+2 {
			t.Fatalf("columns %d, want %d", len(got[i]), len(before[0])+2)
		}
		if ua, fl := got[i][len(before[0])], got[i][len(before[0])+1]; ua != "" || fl != int64(0) {
			t.Fatalf("new columns default to %v %v", ua, fl)
		}
		got[i] = got[i][:len(before[0])]
	}
	if !reflect.DeepEqual(got, before) {
		t.Fatalf("saved Source rows changed:\n%v\n%v", got, before)
	}
	var n int
	if err := s.Read().QueryRow("SELECT count(*) FROM releases WHERE id=31 AND source_id=5").Scan(&n); err != nil || n != 1 {
		t.Fatalf("Release kept %d %v", n, err)
	}
	if err := s.Read().QueryRow("SELECT count(*) FROM source_calls WHERE source_id=5").Scan(&n); err != nil || n != 1 {
		t.Fatalf("call record kept %d %v", n, err)
	}
	if fk := downloaderRows(t, s.Read(), "PRAGMA foreign_key_check"); len(fk) != 0 {
		t.Fatalf("foreign key violations=%v", fk)
	}
	write := func(q string) error {
		return s.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(q); return err })
	}
	for _, site := range []string{"iptorrents", "knaben", "subsplease", "torrentscsv"} {
		if err := write(`INSERT INTO sources(created_at,created_by,updated_at,updated_by,name,protocol,format,url,user_agent,freeleech_only) VALUES('t','{}','t','{}','` + site + `','torrent','` + site + `','https://` + site + `.example','Mozilla/5.0',1)`); err != nil {
			t.Fatalf("%s: %v", site, err)
		}
	}
	if err := write(`UPDATE sources SET format='jackett' WHERE id=5`); err == nil {
		t.Fatal("format check lost")
	}
	if err := write(`DELETE FROM sources WHERE id=5`); err != nil {
		t.Fatal(err)
	}
	if err := s.Read().QueryRow("SELECT count(*) FROM releases WHERE source_id=5").Scan(&n); err != nil || n != 0 {
		t.Fatalf("Releases of a removed Source left %d %v", n, err)
	}
	if err := write(`UPDATE sources SET state='enabled' WHERE id=9`); err != nil {
		t.Fatal(err)
	}
	if err := s.Read().QueryRow("SELECT live FROM import_created WHERE tbl='sources' AND item_id=9").Scan(&n); err != nil || n != 0 {
		t.Fatalf("import mark trigger lost: live=%d %v", n, err)
	}
}
