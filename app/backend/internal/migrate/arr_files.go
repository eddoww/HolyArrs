package migrate

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Radarr and Sonarr record every video file they hold with the quality
// they judged it, and Sonarr which episodes each file holds and which
// episodes are watched. The move reads them, so a title comes over with
// the files and quality the old app had instead of guessing them from the
// file names again.

// episodeFiles reads Sonarr's episode files and episodes through its API.
// Both lists are asked per show (the API wants a show id); a show whose
// lists cannot be read comes over without them, with a note.
func (r arrReader) episodeFiles(ctx context.Context, c *apiClient, out *arrRows) error {
	failed := 0
	for _, show := range out.sets["titles"] {
		id := num(show["id"])
		if id == 0 {
			continue
		}
		q := url.Values{"seriesId": {strconv.FormatInt(id, 10)}}
		var files, eps []map[string]any
		err := c.getJSON(ctx, r.apiBase()+"/episodefile", q, &files)
		if err == nil {
			err = c.getJSON(ctx, r.apiBase()+"/episode", q, &eps)
		}
		if err != nil {
			if !errors.Is(err, ErrBadAnswer) {
				return err
			}
			failed++
			continue
		}
		if len(files)+len(eps)+len(out.sets["episodefiles"])+len(out.sets["episodes"]) > c.lim.MaxRows {
			return ErrTooBig
		}
		for _, f := range files {
			m := lower(f)
			m["seriesid"] = id
			out.sets["episodefiles"] = append(out.sets["episodefiles"], m)
		}
		for _, e := range eps {
			m := lower(e)
			m["seriesid"] = id
			out.sets["episodes"] = append(out.sets["episodes"], m)
		}
	}
	if failed > 0 {
		out.notes = append(out.notes, strconv.Itoa(failed)+" shows' episode files could not be read; those shows come over by their file names")
	}
	return nil
}

// fileQuality reads a recorded file's quality: an API object
// ({"quality": {"name", "source", "resolution", "modifier"}}) or a
// backup's ({"quality": <id>}, named through QualityDefinitions).
func fileQuality(v any, quality map[int64]string) (name, source string, resolution int, modifier string) {
	q := lower(obj(v))
	inner := q["quality"]
	if m := obj(inner); m != nil {
		m = lower(m)
		return str(m["name"]), str(m["source"]), int(num(m["resolution"])), str(m["modifier"])
	}
	if id := num(inner); id != 0 {
		return quality[id], "", 0, ""
	}
	return "", "", 0, ""
}

// recordedFile builds one TitleFile from a movie or episode file row.
// dir is the title's folder, for a backup row that holds only the path
// inside it.
func recordedFile(m map[string]any, dir string, quality map[int64]string) TitleFile {
	f := TitleFile{ID: num(m["id"]), Path: str(m["path"]), Size: num(m["size"])}
	if f.Path == "" && dir != "" {
		if rel := str(m["relativepath"]); rel != "" {
			f.Path = strings.TrimRight(dir, `/\`) + "/" + strings.ReplaceAll(rel, `\`, "/")
		}
	}
	f.Quality, f.Source, f.Resolution, f.Modifier = fileQuality(m["quality"], quality)
	f.Quality, f.Source, f.Modifier = clip(f.Quality, 60), clip(f.Source, 30), clip(f.Modifier, 30)
	return f
}

// attachFiles puts each recorded file and episode on its title: Radarr's
// movie file (on the movie row through the API, the MovieFiles table in a
// backup), Sonarr's episode files and episodes.
func (r arrReader) attachFiles(s *Snapshot, in *arrRows, quality map[int64]string) {
	byID := map[int64]int{}
	for i, t := range s.Titles {
		byID[t.SourceID] = i
	}
	switch r.kind {
	case Radarr:
		for _, m := range in.sets["titles"] {
			if mf := obj(m["moviefile"]); mf != nil {
				if i, ok := byID[num(m["id"])]; ok {
					s.Titles[i].Files = append(s.Titles[i].Files, recordedFile(lower(mf), s.Titles[i].Path, quality))
				}
			}
		}
		for _, m := range in.sets["moviefiles"] {
			if i, ok := byID[num(m["movieid"])]; ok {
				s.Titles[i].Files = append(s.Titles[i].Files, recordedFile(m, s.Titles[i].Path, quality))
			}
		}
	case Sonarr:
		for _, m := range in.sets["episodefiles"] {
			if i, ok := byID[num(m["seriesid"])]; ok {
				s.Titles[i].Files = append(s.Titles[i].Files, recordedFile(m, s.Titles[i].Path, quality))
			}
		}
		for _, m := range in.sets["episodes"] {
			i, ok := byID[num(m["seriesid"])]
			if !ok {
				continue
			}
			e := Episode{Season: int(num(m["seasonnumber"])), Number: int(num(m["episodenumber"])), Monitored: boolean(m["monitored"]),
				FileID: num(m["episodefileid"])}
			if e.Monitored && e.FileID == 0 {
				continue
			}
			s.Titles[i].Episodes = append(s.Titles[i].Episodes, e)
		}
	}
}

// qualityWords spells a recorded quality in the words the release name
// reader knows ("2160p WEB-DL", "1080p BluRay REMUX"), from its parts when
// the app gave them, else from its name. "" when it says nothing usable
// (Unknown, or a source HolyArrs does not tell apart).
func qualityWords(f TitleFile) string {
	name := strings.ToLower(f.Quality)
	source, modifier, res := strings.ToLower(f.Source), strings.ToLower(f.Modifier), f.Resolution
	if res == 0 {
		if m := reQualityRes.FindStringSubmatch(name); m != nil {
			res, _ = strconv.Atoi(m[1])
		}
	}
	if source == "" {
		for _, w := range []struct{ word, source, modifier string }{
			{"remux", "bluray", "remux"}, {"br-disk", "bluray", "brdisk"}, {"bluray", "bluray", ""}, {"webdl", "webdl", ""},
			{"webrip", "webrip", ""}, {"hdtv", "tv", ""}, {"sdtv", "tv", ""}, {"raw-hd", "tv", ""}, {"dvd", "dvd", ""},
			{"cam", "cam", ""}, {"telesync", "telesync", ""}, {"telecine", "telecine", ""},
		} {
			if strings.Contains(name, w.word) {
				source, modifier = w.source, w.modifier
				break
			}
		}
	}
	var words string
	switch {
	case modifier == "remux" || source == "blurayraw":
		words = "BluRay REMUX"
	case modifier == "brdisk":
		words = "BDMV"
	case modifier == "rawhd":
		words = "HDTV"
	case source == "bluray":
		words = "BluRay"
	case source == "webdl" || source == "web":
		words = "WEB-DL"
	case source == "webrip":
		words = "WEBRip"
	case source == "tv" || source == "television" || source == "televisionraw":
		words = "HDTV"
	case source == "dvd":
		words = "DVD"
	case source == "cam":
		words = "CAM"
	case source == "telesync" || source == "telecine":
		words = "TELESYNC"
	}
	// An episode name is read for quality only when it holds a resolution,
	// so SD television and DVD copies say 480p.
	if res == 0 && (words == "HDTV" || words == "DVD") {
		res = 480
	}
	if res > 0 && words != "" {
		return strconv.Itoa(res) + "p " + words
	}
	if res > 0 {
		return strconv.Itoa(res) + "p"
	}
	return words
}

var reQualityRes = regexp.MustCompile(`(\d{3,4})p`)
