package downloads

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// LinkIn is the body of a fetch by link: the web address, and which copy it
// is for. A movie may leave both ids out when it has one copy; a show names
// the episode with item_id.
type LinkIn struct {
	URL    string `json:"url"`
	SlotID *int64 `json:"slot_id,omitempty"`
	ItemID *int64 `json:"item_id,omitempty"`
}

func (a *Web) linkRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/v1/titles/{id}/link", a.fetchLink)
}

// fetchLink sends a web link (any site yt-dlp reads) for one copy of a title
// to the yt-dlp Downloader. The file is imported like any other download.
func (a *Web) fetchLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var t mediafiles.Title
	if err == nil {
		err = mediafiles.LoadTitle(r.Context(), a.Store.Read(), id, &t)
	}
	if err != nil {
		webserver.Fail(w, http.StatusNotFound, "no such Title")
		return
	}
	var in LinkIn
	if !webserver.Decode(w, r, &in, `body is {"url": string, "slot_id": int, "item_id": int}; slot_id and item_id are optional for a movie with one copy`) {
		return
	}
	if t.Kind != "movie" && t.Kind != "show" {
		webserver.Fail(w, http.StatusBadRequest, "links can be fetched for movies and episodes")
		return
	}
	slots, err := mediafiles.SlotsOfTitle(r.Context(), a.Store.Read(), t.ID)
	if err != nil {
		webserver.Internal(w, "downloads", err)
		return
	}
	var picked []mediafiles.Slot
	for _, s := range slots {
		if s.State == mediafiles.SlotRemoved || in.SlotID != nil && s.ID != *in.SlotID || in.ItemID != nil && s.ItemID != *in.ItemID {
			continue
		}
		picked = append(picked, s)
	}
	switch {
	case len(picked) == 0:
		webserver.Fail(w, http.StatusNotFound, "no such copy of this title")
		return
	case len(picked) > 1 && in.SlotID == nil:
		webserver.Fail(w, http.StatusBadRequest, "this title has more than one copy or episode: say which with slot_id or item_id")
		return
	}
	dl, info, err := a.Manager().GrabLink(r.Context(), picked[0].ID, in.URL, actor(r))
	switch {
	case err == nil:
		webserver.WriteJSON(w, http.StatusOK, map[string]any{"download_id": dl, "slot_id": picked[0].ID, "title": info.Title, "site": info.Site, "duration": info.Duration})
	case errors.Is(err, ErrYtdlpLink), errors.Is(err, ErrYtdlpNoRoot):
		webserver.Fail(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNoYtdlp), errors.Is(err, ErrLinkBusy), errors.Is(err, mediafiles.ErrLookOnly):
		webserver.Fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, database.ErrNotFound):
		webserver.Fail(w, http.StatusNotFound, "no such copy of this title")
	case errors.Is(err, ErrLinkRefused):
		// yt-dlp's own refusal (unsupported site, removed video) is for the person to read.
		webserver.Fail(w, http.StatusUnprocessableEntity, err.Error())
	default:
		webserver.Internal(w, "downloads", err)
	}
}
