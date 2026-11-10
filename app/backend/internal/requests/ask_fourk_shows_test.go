package requests

import (
	"context"
	"errors"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/library"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// fourKShowSetup adds a 4K show Library folder and 4K show Pick rules, and
// sets them as the Requests settings for 4K show asks.
func fourKShowSetup(t *testing.T, r *rig) (folder, rules int64) {
	t.Helper()
	ctx := context.Background()
	lf, err := library.AddFolder(ctx, r.st, library.FolderInput{Path: t.TempDir(), Kind: "show", Label: "TV-4K"}, nil, domain.System)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.st.Read().QueryRow("SELECT id FROM pick_rules WHERE name = ?", mediafiles.GoodEverydayName).Scan(&rules); err != nil {
		t.Fatal(err)
	}
	set, err := ReadSettings(ctx, r.st.Read())
	if err != nil {
		t.Fatal(err)
	}
	set.ShowFourKLibraryFolderID, set.ShowFourKPickRulesID = lf.ID, rules
	if err := SaveSettings(ctx, r.st, set, domain.System); err != nil {
		t.Fatal(err)
	}
	return lf.ID, rules
}

// A 4K show ask mirrors a 4K movie ask: it is its own ask beside a plain
// one, and approving a 4K ask for a show not in the Library adds the show
// in the 4K Library folder on the 4K Pick rules, as its 4K Version, with
// only the asked seasons kept up to date.
func TestFourKShowAskAddsTheShowAsItsFourKVersion(t *testing.T) {
	r := showRig(t)
	folder, rules := fourKShowSetup(t, r)
	ctx := context.Background()
	plain, err := r.svc.AskShow(ctx, r.ana, "tmdb:100", []int{1})
	if err != nil {
		t.Fatal(err)
	}
	uhd, err := r.svc.AskShow4K(ctx, r.bo, "tvdb:200", []int{2}, nil, false)
	if err != nil || uhd.Joined || uhd.Ask.ID == plain.Ask.ID || !uhd.Ask.FourK || len(uhd.Ask.Seasons) != 1 || uhd.Ask.Seasons[0] != 2 {
		t.Fatalf("4K ask %+v %v", uhd, err)
	}
	// Another 4K ask for the same season joins the 4K one, not the plain one.
	again, err := r.svc.AskShow4K(ctx, r.ana, "tmdb:100", []int{2}, nil, false)
	if err != nil || !again.Joined || again.Ask.ID != uhd.Ask.ID {
		t.Fatalf("4K join %+v %v", again, err)
	}
	got, err := r.svc.Approve(ctx, uhd.Ask.ID, r.luis, ApproveOptions{})
	if err != nil || got.TitleID == nil {
		t.Fatalf("approve %+v %v", got, err)
	}
	vs, _ := database.Select[mediafiles.Version](ctx, r.st.Read(), "versions", "title_id = ?", *got.TitleID)
	if len(vs) != 1 || vs[0].Label != mediafiles.FourKName || vs[0].LibraryFolderID != folder || vs[0].PickRulesID != rules {
		t.Fatalf("versions %+v", vs)
	}
	keeps := seasonKeeps(t, r.st, *got.TitleID)
	if keeps[1] || !keeps[2] {
		t.Fatalf("season switches %v", keeps)
	}
	// The plain ask still waits, apart from the 4K one.
	if a, _ := get(ctx, r.st.Read(), plain.Ask.ID); a.State != domain.AskAsked {
		t.Fatalf("plain ask %+v", a)
	}
	// While the approved 4K ask is open, asking for season 2 in 4K again joins it.
	if res, err := r.svc.AskShow4K(ctx, r.ana, "tmdb:100", []int{2}, nil, false); err != nil || res.Ask.ID != uhd.Ask.ID {
		t.Fatalf("4K again %+v %v", res, err)
	}
}

// A 4K ask for a show in the Library with no 4K Version adds one beside
// the main copy, in the 4K folder, and keeps the asked season up to date.
func TestFourKShowAskAddsAFourKVersionBesideTheMainCopy(t *testing.T) {
	r := showRig(t)
	folder, _ := fourKShowSetup(t, r)
	ctx := context.Background()
	plain, err := r.svc.AskShow(ctx, r.ana, "tmdb:100", []int{1})
	if err != nil {
		t.Fatal(err)
	}
	main, err := r.svc.Approve(ctx, plain.Ask.ID, r.luis, ApproveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if has, _ := HasFourK(ctx, r.st.Read(), *main.TitleID); has {
		t.Fatal("a plain ask added a 4K Version")
	}
	uhd, err := r.svc.AskShow4K(ctx, r.bo, "tmdb:100", []int{1}, nil, false)
	if err != nil || !uhd.Ask.FourK || uhd.Ask.State != domain.AskAsked {
		t.Fatalf("4K on owned %+v %v", uhd, err)
	}
	if _, err := r.svc.Approve(ctx, uhd.Ask.ID, r.luis, ApproveOptions{}); err != nil {
		t.Fatal(err)
	}
	vid, err := fourKVersion(ctx, r.st.Read(), *main.TitleID)
	if err != nil || vid == 0 {
		t.Fatalf("4K Version %d %v", vid, err)
	}
	var v mediafiles.Version
	if err := database.Get(ctx, r.st.Read(), "versions", vid, &v); err != nil || v.LibraryFolderID != folder || v.IsMain {
		t.Fatalf("4K Version %+v %v", v, err)
	}
	// The 4K ask's step and readiness look at the 4K Version only.
	vs, _ := r.svc.Views(ctx, "id = ?", uhd.Ask.ID)
	if len(vs) != 1 || !vs[0].FourK {
		t.Fatalf("view %+v", vs)
	}
	if ready, err := showReady(ctx, r.st.Read(), *main.TitleID, []int{1}, nil, true, r.clock); err != nil || ready {
		t.Fatalf("4K ready %v %v", ready, err)
	}
}

// The 4K Requests settings are kept and checked: a 4K show folder must be a
// show folder.
func TestFourKRequestSettings(t *testing.T) {
	r := showRig(t)
	ctx := context.Background()
	folder, rules := fourKShowSetup(t, r)
	set, err := ReadSettings(ctx, r.st.Read())
	if err != nil || set.ShowFourKLibraryFolderID != folder || set.ShowFourKPickRulesID != rules {
		t.Fatalf("settings %+v %v", set, err)
	}
	set.FourKLibraryFolderID = folder
	if err := SaveSettings(ctx, r.st, set, domain.System); !errors.Is(err, library.ErrNotMovieFolder) {
		t.Fatalf("a show folder for 4K movies: %v", err)
	}
	set.FourKLibraryFolderID, set.FourKPickRulesID = 0, 99999
	if err := SaveSettings(ctx, r.st, set, domain.System); !errors.Is(err, library.ErrNoPickRules) {
		t.Fatalf("unknown 4K Pick rules: %v", err)
	}
}

// A 4K movie ask uses the 4K Requests settings: the copy goes in the 4K
// movie folder on the 4K Pick rules, and a movie added by it is its 4K
// Version.
func TestFourKMovieAskUsesTheFourKSettings(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	lf, err := library.AddFolder(ctx, r.st, library.FolderInput{Path: t.TempDir(), Kind: "movie", Label: "Movies-4k"}, nil, domain.System)
	if err != nil {
		t.Fatal(err)
	}
	var rules int64
	_ = r.st.Read().QueryRow("SELECT id FROM pick_rules WHERE name = ?", mediafiles.GoodEverydayName).Scan(&rules)
	set, _ := ReadSettings(ctx, r.st.Read())
	set.FourKLibraryFolderID, set.FourKPickRulesID = lf.ID, rules
	if err := SaveSettings(ctx, r.st, set, domain.System); err != nil {
		t.Fatal(err)
	}
	uhd, err := r.svc.Ask4K(ctx, r.ana, "693134")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.svc.Approve(ctx, uhd.Ask.ID, r.luis, ApproveOptions{})
	if err != nil || got.TitleID == nil {
		t.Fatalf("approve %+v %v", got, err)
	}
	vs, _ := database.Select[mediafiles.Version](ctx, r.st.Read(), "versions", "title_id = ?", *got.TitleID)
	if len(vs) != 1 || vs[0].Label != mediafiles.FourKName || vs[0].LibraryFolderID != lf.ID || vs[0].PickRulesID != rules {
		t.Fatalf("versions %+v", vs)
	}
	if has, _ := HasFourK(ctx, r.st.Read(), *got.TitleID); !has {
		t.Fatal("the movie added by a 4K ask is not its 4K copy")
	}
}
