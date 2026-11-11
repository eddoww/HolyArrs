-- A subtitle file beside the video that names no language counts, as in
-- Bazarr, when its language is clearly guessed from its text; the row says
-- so, and the title's subtitle list shows it.
ALTER TABLE subtitles ADD COLUMN language_guessed INTEGER NOT NULL DEFAULT 0;
