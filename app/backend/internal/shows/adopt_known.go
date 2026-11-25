package shows

import (
	"github.com/HolyArrs/holyarrs/app/backend/internal/library"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// withKnownEpisodes puts the episodes, quality and release name an old app
// recorded on the files it recorded, in place of what their names give. A file the old
// app did not record keeps its name's episodes, less any a recorded file
// holds.
func withKnownEpisodes(files []library.AdoptEpisodeFile, known []library.KnownFile) []library.AdoptEpisodeFile {
	byPath := map[string]library.KnownFile{}
	taken := map[[2]int]bool{}
	for _, k := range known {
		if len(k.Episodes) == 0 {
			continue
		}
		byPath[k.Path] = k
	}
	for i, f := range files {
		k, ok := byPath[f.Path]
		if !ok {
			continue
		}
		files[i].Keys, files[i].Episodes, files[i].Why, files[i].Quality, files[i].ReleaseName = nil, []string{}, "", k.Quality, k.ReleaseName
		for _, e := range k.Episodes {
			if taken[e] {
				continue
			}
			taken[e] = true
			files[i].Keys = append(files[i].Keys, e)
			files[i].Episodes = append(files[i].Episodes, mediafiles.EpisodeRef{Season: e[0], Episode: e[1]}.Label())
		}
	}
	for i, f := range files {
		if _, ok := byPath[f.Path]; ok || len(f.Keys) == 0 {
			continue
		}
		var keys [][2]int
		var labels []string
		for j, e := range f.Keys {
			if !taken[e] {
				keys = append(keys, e)
				labels = append(labels, f.Episodes[j])
			}
		}
		if len(keys) == 0 {
			files[i].Why = "the old app recorded another file for its episodes"
		}
		if labels == nil {
			labels = []string{}
		}
		files[i].Keys, files[i].Episodes = keys, labels
	}
	return files
}
