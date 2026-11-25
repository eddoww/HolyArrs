package downloads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

var (
	// ErrNoYtdlp means no yt-dlp Downloader is set up to take a link.
	ErrNoYtdlp = errors.New("add a yt-dlp Downloader first (Settings > Downloaders)")
	// ErrLinkBusy means the Slot already has a Download or a Release on its way.
	ErrLinkBusy = errors.New("this copy is already being fetched or imported; wait for it, or remove that download first")
	// ErrLinkRefused starts every reason yt-dlp gives for not taking a link.
	ErrLinkRefused = errors.New("yt-dlp can't fetch this link")
)

// LinkInfo is what yt-dlp says about a link before anything is fetched.
type LinkInfo struct {
	Title    string  `json:"title"`
	Site     string  `json:"site"`
	Duration float64 `json:"duration"`
}

// linkStart are the Slot states a link may be sent from: nothing is on its way
// and no search is choosing for it right now.
var linkStart = map[mediafiles.SlotState]bool{
	mediafiles.SlotNotOutYet: true, mediafiles.SlotWanted: true, mediafiles.SlotHeld: true, mediafiles.SlotFailed: true,
	mediafiles.SlotNeedsADecision: true, mediafiles.SlotRejected: true, mediafiles.SlotInLibrary: true, mediafiles.SlotBetterWanted: true,
}

// Probe asks yt-dlp what a link holds without fetching it, so an unsupported
// site or a removed video is refused before a Download exists.
func (y *Ytdlp) Probe(ctx context.Context, link string) (LinkInfo, error) {
	y.init()
	link, err := ytdlpLink(link)
	if err != nil {
		return LinkInfo{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	a := []string{"--ignore-config"}
	if c := y.own("cookies.txt"); c != "" {
		a = append(a, "--cookies", c)
	}
	a = append(a, "--no-playlist", "--skip-download", "--dump-single-json", "--no-warnings", "--", link)
	cmd := y.command(ctx, y.binary(), a...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return LinkInfo{}, fmt.Errorf("%w: it took too long to read", ErrLinkRefused)
		}
		said := ""
		for _, ln := range strings.Split(stderr.String(), "\n") {
			if s, ok := strings.CutPrefix(strings.TrimSpace(ln), "ERROR:"); ok {
				said = strings.TrimSpace(s)
			}
		}
		return LinkInfo{}, fmt.Errorf("%w: %s", ErrLinkRefused, ytFailure(err, said))
	}
	var v struct {
		Title    string  `json:"title"`
		Extract  string  `json:"extractor_key"`
		Duration float64 `json:"duration"`
		Type     string  `json:"_type"`
	}
	if json.Unmarshal(out, &v) != nil {
		return LinkInfo{}, fmt.Errorf("%w: its answer could not be read", ErrLinkRefused)
	}
	if v.Type == "playlist" {
		return LinkInfo{}, fmt.Errorf("%w: it is a playlist or channel; give the link of one video", ErrLinkRefused)
	}
	return LinkInfo{Title: clipText(strings.TrimSpace(v.Title), 200), Site: clipText(v.Extract, 60), Duration: v.Duration}, nil
}

// GrabLink sends a web link for one Slot to the yt-dlp Downloader, records
// the Download with no Release, and moves the Slot to downloading. The normal
// Import takes the file once yt-dlp is done.
func (m *Manager) GrabLink(ctx context.Context, slotID int64, link string, by domain.Actor) (int64, LinkInfo, error) {
	if m.Ytdlp == nil {
		return 0, LinkInfo{}, ErrNoYtdlp
	}
	link, err := ytdlpLink(link)
	if err != nil {
		return 0, LinkInfo{}, err
	}
	db := m.Store.Read()
	var sl mediafiles.Slot
	if err := mediafiles.LoadSlot(ctx, db, slotID, &sl); err != nil {
		return 0, LinkInfo{}, fmt.Errorf("slot %d: %w", slotID, err)
	}
	if !linkStart[sl.State] {
		return 0, LinkInfo{}, ErrLinkBusy
	}
	if mediafiles.SlotLookOnly(ctx, db, slotID) {
		return 0, LinkInfo{}, mediafiles.ErrLookOnly
	}
	all, err := webDownloaders(ctx, db)
	if err != nil {
		return 0, LinkInfo{}, err
	}
	if len(all) == 0 {
		return 0, LinkInfo{}, ErrNoYtdlp
	}
	d := all[0]
	if readOnlyFor(d) {
		return 0, LinkInfo{}, errors.New(d.Name + " is read-only, so HolyArrs can't send to it")
	}
	info, err := m.Ytdlp.Probe(ctx, link)
	if err != nil {
		return 0, LinkInfo{}, err
	}
	kind, _ := mediafiles.SlotKind(ctx, db, slotID)
	name := info.Title
	if name == "" {
		name = link
	}
	e, err := m.EngineFor(ctx, d)
	if err != nil {
		return 0, LinkInfo{}, err
	}
	ext, err := e.Add(ctx, AddRequest{URL: link, Name: name, Category: d.CategoryFor(kind)})
	if err != nil {
		return 0, LinkInfo{}, err
	}
	now := m.now()
	dl := &domain.Download{DownloaderID: d.ID, SlotIDs: []int64{slotID}, ExternalID: ext, State: "sent",
		FilesSeen: []domain.SeenFile{}, SavePath: m.SavePath, StartedBy: by, ProgressAt: &now}
	reason := "sent " + name + " to " + d.Name
	err = m.Store.Write(ctx, func(tx *sql.Tx) error {
		var current mediafiles.Slot
		if err := mediafiles.LoadSlot(ctx, tx, slotID, &current); err != nil {
			return err
		}
		if !linkStart[current.State] || !current.CreatedAt.Equal(sl.CreatedAt) {
			return ErrLinkBusy
		}
		old, inUse, err := heldBy(ctx, tx, d.ID, ext)
		if err != nil {
			return err
		}
		if inUse {
			return ErrLinkBusy
		}
		if old > 0 {
			dl.ID = old
			if err := updateReused(tx, dl, by); err != nil {
				return err
			}
		} else if _, err := database.Insert(tx, "downloads", dl, by); err != nil {
			return err
		}
		// The Slot walks the usual path to downloading in one write: a link is
		// a choice made by hand, with no Release behind it.
		for _, to := range linkPath(current.State) {
			if err := mediafiles.MoveSlotTx(tx, slotID, to, reason, by); err != nil {
				return err
			}
		}
		return mediafiles.SetSlotLinkDownloadTx(tx, slotID, dl.ID)
	})
	if err != nil {
		// A job no Download points at must not go on by itself.
		if used, err := jobInUse(ctx, db, d.ID, ext); err == nil && !used {
			_ = e.Remove(context.WithoutCancel(ctx), ext, true)
		}
		return 0, LinkInfo{}, err
	}
	m.event(ctx, dl, "download_sent", fmt.Sprintf("Sent %s to %s", name, d.Name))
	m.publish("slot", map[string]any{"id": slotID, "state": mediafiles.SlotDownloading})
	m.publish("download", dl)
	m.Poke()
	m.Kick(d.ID)
	return dl.ID, info, nil
}

// linkPath is the Slot moves from a starting state to downloading.
func linkPath(from mediafiles.SlotState) []mediafiles.SlotState {
	switch from {
	case mediafiles.SlotHeld, mediafiles.SlotNeedsADecision:
		return []mediafiles.SlotState{mediafiles.SlotChosen, mediafiles.SlotDownloading}
	}
	return []mediafiles.SlotState{mediafiles.SlotSearching, mediafiles.SlotChosen, mediafiles.SlotDownloading}
}
