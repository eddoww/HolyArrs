package requests

// openShow4KAskSQL is openShowAskSQL narrowed to asks for the 4K line.
const openShow4KAskSQL = `SELECT a.id, a.state, a.seasons, EXISTS (SELECT 1 FROM ask_people p WHERE p.ask_id = a.id AND p.user_id = ?),
	(SELECT count(*) FROM ask_people p WHERE p.ask_id = a.id) FROM asks a WHERE a.kind = 'show' AND a.state IN ('asked', 'approved')
	AND a.quality = '` + QualityFourK + `' AND (`
