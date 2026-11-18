package migrate

import "database/sql"

// made says whether a live (not undone) move already made key, and its row.
func (r *run) made(key string) (int64, bool) {
	var id int64
	var what, marked string
	err := r.m.Store.Read().QueryRowContext(r.ctx, `SELECT m.row_id, m.what, m.fingerprint FROM move_made m JOIN moves v ON v.id = m.move_id
		WHERE m.key = ? AND v.state != 'undone' ORDER BY m.rowid DESC LIMIT 1`, key).Scan(&id, &what, &marked)
	if err != nil {
		return 0, false
	}
	return r.validateMade(id, what, marked)
}

func (r *run) madeThisMove(key string) (int64, bool) {
	var id int64
	var what, marked string
	err := r.m.Store.Read().QueryRowContext(r.ctx, `SELECT row_id, what, fingerprint FROM move_made
		WHERE move_id = ? AND key = ? ORDER BY rowid DESC LIMIT 1`, r.move, key).Scan(&id, &what, &marked)
	if err != nil {
		return 0, false
	}
	return r.validateMade(id, what, marked)
}

func (r *run) validateMade(id int64, what, marked string) (int64, bool) {
	fingerprintKind := markFingerprintKind(what)
	if _, rowIdentity := natural[fingerprintKind]; !rowIdentity {
		return id, true
	}
	// Natural-row marks written before fingerprints existed cannot prove that
	// a recycled SQLite id still belongs to the imported person or other row.
	// Retrying conservatively is safer than attaching ownership to a stranger.
	if marked == "" {
		return 0, false
	}
	current := fingerprint(r.ctx, r.m.Store.Read(), fingerprintKind, id)
	if current == "" {
		return 0, false
	}
	if what == "title" || what == "music_policy" {
		var state string
		if r.m.Store.Read().QueryRowContext(r.ctx, "SELECT state FROM titles WHERE id = ?", id).Scan(&state) != nil || state == "removed" {
			return 0, false
		}
		if current != marked {
			return 0, false
		}
	} else if !sameMarkedCreation(marked, current) {
		return 0, false
	}
	return id, true
}

func (r *run) madeItem(it Item) (int64, bool) {
	if id, ok := r.made(it.Key); ok {
		return id, true
	}
	// Numeric person aliases predate stable provider identities. They cannot
	// prove that a reused source id still names the same person, so request
	// ownership must never be recovered through them.
	if it.Person != nil {
		return 0, false
	}
	for _, key := range it.LegacyKeys {
		if id, ok := r.made(key); ok {
			return id, true
		}
	}
	return 0, false
}

// folderKeptAtUndo says the Library folder id is the one an undone move
// made for key, and gives its default Pick rules (0 for none) when they are
// still none or ones that move made: a new move may point it at its own.
func (r *run) folderKeptAtUndo(key string, id int64) (int64, bool) {
	db := r.m.Store.Read()
	var move int64
	var marked string
	err := db.QueryRowContext(r.ctx, `SELECT m.move_id, m.fingerprint FROM move_made m JOIN moves v ON v.id = m.move_id
		WHERE m.key = ? AND m.what = 'folder' AND m.row_id = ? AND v.state = 'undone' ORDER BY m.rowid DESC LIMIT 1`, key, id).Scan(&move, &marked)
	if err != nil || marked == "" || fingerprint(r.ctx, db, "folder", id) != marked || r.hasActiveMark(key) {
		return 0, false
	}
	var rules sql.NullInt64
	if db.QueryRowContext(r.ctx, "SELECT default_pick_rules_id FROM library_folders WHERE id = ?", id).Scan(&rules) != nil {
		return 0, false
	}
	if !rules.Valid {
		return 0, true
	}
	var n int
	_ = db.QueryRowContext(r.ctx, "SELECT count(*) FROM move_made WHERE move_id = ? AND what = 'pick_rules' AND row_id = ?", move, rules.Int64).Scan(&n)
	return rules.Int64, n > 0
}

func (r *run) hasActiveMark(key string) bool {
	var one int
	err := r.m.Store.Read().QueryRowContext(r.ctx, `SELECT 1 FROM move_made m JOIN moves v ON v.id=m.move_id
		WHERE m.key=? AND v.state!='undone' LIMIT 1`, key).Scan(&one)
	return err == nil
}
