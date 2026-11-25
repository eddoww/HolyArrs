package downloads

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeYtdlp writes a shell script that acts like yt-dlp: --version prints a
// version, --dump-single-json prints one video, and a fetch writes a file into
// the -P folder after two progress lines. A link holding "fail" fails, and one
// holding "slow" waits until it is stopped.
func fakeYtdlp(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake yt-dlp is a shell script")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
trap 'exit 130' INT
url=""; out=""; dump=0
while [ $# -gt 0 ]; do
  case "$1" in
    --version) echo "2026.01.01"; exit 0 ;;
    --dump-single-json) dump=1 ;;
    -P) shift; out="$1" ;;
    --) shift; url="$1" ;;
  esac
  shift
done
case "$url" in *fail*) echo "ERROR: [generic] Unsupported URL: $url" >&2; exit 1 ;; esac
case "$url" in *list*) echo '{"_type":"playlist","title":"A list"}'; exit 0 ;; esac
if [ $dump = 1 ]; then echo '{"title":"A video","extractor_key":"Fake","duration":61.5}'; exit 0; fi
echo "[holyarrs] 50 100 NA 25.0 2"
case "$url" in *slow*) while :; do sleep 0.05; done ;; esac
echo "[holyarrs] 100 100 NA 0 0"
echo "[Merger] Merging formats"
echo "video" > "$out/A video [abc].mkv"
echo "part" > "$out/A video [abc].f1.mp4.part"
exit 0
`
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func newTestYtdlp(t *testing.T) (*Ytdlp, string) {
	root := t.TempDir()
	y := &Ytdlp{Root: func() string { return root }, Binary: fakeYtdlp(t), ConfigDir: t.TempDir()}
	t.Cleanup(y.Wait)
	return y, root
}

func waitState(t *testing.T, e *YtdlpEngine, id, want string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		j, err := e.Get(context.Background(), id)
		if err == nil && j.State == want {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s: state %q (err %v), want %q", id, j.State, err, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestYtdlpFetchesIntoItsOwnFolder(t *testing.T) {
	y, root := newTestYtdlp(t)
	e := y.Engine("holyarrs", nil, "")
	ctx := context.Background()
	h, err := e.Health(ctx)
	if err != nil || h.Version != "yt-dlp 2026.01.01" {
		t.Fatalf("health %+v, %v", h, err)
	}
	id, err := e.Add(ctx, AddRequest{URL: "https://video.example/watch?v=abc", Name: "A video", Category: "holyarrs"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := YtdlpID(id); err != nil {
		t.Fatalf("id %q is not a yt-dlp id", id)
	}
	j := waitState(t, e, id, StateFinished)
	if want := filepath.Join(root, "yt-dlp", "holyarrs", id); j.SavePath != want {
		t.Fatalf("save path %q, want %q", j.SavePath, want)
	}
	if len(j.Files) != 1 || j.Files[0].Name != "A video [abc].mkv" || j.Progress != 1 {
		t.Fatalf("files %+v progress %v: the partial file must be left out", j.Files, j.Progress)
	}
	again, err := e.Add(ctx, AddRequest{URL: "https://video.example/watch?v=abc", Category: "holyarrs"})
	if err != nil || again != id {
		t.Fatalf("the same link gave %q, %v; want the earlier job %q", again, err, id)
	}
	list, err := e.List(ctx, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("list %+v, %v", list, err)
	}
	if other, _ := e.List(ctx, "else"); len(other) != 0 {
		t.Fatalf("another category lists %+v", other)
	}
}

func TestYtdlpFailureKeepsItsReason(t *testing.T) {
	y, _ := newTestYtdlp(t)
	e := y.Engine("holyarrs", nil, "")
	id, err := e.Add(context.Background(), AddRequest{URL: "https://video.example/fail", Category: "holyarrs"})
	if err != nil {
		t.Fatal(err)
	}
	j := waitState(t, e, id, StateFailed)
	if !strings.Contains(j.Detail, "Unsupported URL") {
		t.Fatalf("detail %q should carry yt-dlp's error", j.Detail)
	}
}

func TestYtdlpPauseResumeAndRemove(t *testing.T) {
	y, root := newTestYtdlp(t)
	e := y.Engine("holyarrs", nil, "")
	ctx := context.Background()
	id, err := e.Add(ctx, AddRequest{URL: "https://video.example/slow", Category: "holyarrs"})
	if err != nil {
		t.Fatal(err)
	}
	j := waitState(t, e, id, StateDownloading)
	deadline := time.Now().Add(5 * time.Second)
	for j.Progress < 0.5 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		j, _ = e.Get(ctx, id)
	}
	if j.Progress != 0.5 || j.Speed != 25 || j.ETA != 2 {
		t.Fatalf("progress %v speed %v eta %v, want 0.5, 25, 2", j.Progress, j.Speed, j.ETA)
	}
	if err := e.PauseItem(ctx, id); err != nil {
		t.Fatal(err)
	}
	waitState(t, e, id, StatePaused)
	if err := e.ResumeItem(ctx, id); err != nil {
		t.Fatal(err)
	}
	waitState(t, e, id, StateDownloading)
	if err := e.Remove(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "yt-dlp", "holyarrs", id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the job folder should be gone: %v", err)
	}
	if _, err := e.Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a removed job answers %v", err)
	}
}

func TestYtdlpRemembersJobsAcrossRestarts(t *testing.T) {
	y, root := newTestYtdlp(t)
	e := y.Engine("holyarrs", nil, "")
	id, err := e.Add(context.Background(), AddRequest{URL: "https://video.example/watch?v=abc", Category: "holyarrs"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, e, id, StateFinished)
	y.Wait()

	fresh := &Ytdlp{Root: func() string { return root }, Binary: y.Binary}
	t.Cleanup(fresh.Wait)
	j := waitState(t, fresh.Engine("holyarrs", nil, ""), id, StateFinished)
	if len(j.Files) != 1 {
		t.Fatalf("a restarted runner sees files %+v", j.Files)
	}
}

func TestYtdlpRefusesOddLinksAndCategories(t *testing.T) {
	y, _ := newTestYtdlp(t)
	e := y.Engine("holyarrs", nil, "")
	for _, link := range []string{"", "ftp://x.example/a", "file:///etc/passwd", "https://user:pw@x.example/a", "--exec=rm"} {
		if _, err := e.Add(context.Background(), AddRequest{URL: link, Category: "holyarrs"}); !errors.Is(err, ErrYtdlpLink) {
			t.Errorf("link %q: %v, want ErrYtdlpLink", link, err)
		}
	}
	if _, err := e.Add(context.Background(), AddRequest{URL: "https://x.example/a", Category: ".."}); !errors.Is(err, ErrCategory) {
		t.Errorf("category ..: %v, want ErrCategory", err)
	}
	noRoot := &Ytdlp{Root: func() string { return "" }, Binary: y.Binary}
	if _, err := noRoot.Engine("holyarrs", nil, "").Health(context.Background()); !errors.Is(err, ErrYtdlpNoRoot) {
		t.Errorf("no save folder: %v, want ErrYtdlpNoRoot", err)
	}
}

func TestYtdlpProbe(t *testing.T) {
	y, _ := newTestYtdlp(t)
	info, err := y.Probe(context.Background(), "https://video.example/watch?v=abc")
	if err != nil || info.Title != "A video" || info.Site != "Fake" || info.Duration != 61.5 {
		t.Fatalf("probe %+v, %v", info, err)
	}
	if _, err := y.Probe(context.Background(), "https://video.example/fail"); !errors.Is(err, ErrLinkRefused) || !strings.Contains(err.Error(), "Unsupported URL") {
		t.Fatalf("an unsupported link gave %v", err)
	}
	if _, err := y.Probe(context.Background(), "https://video.example/list"); !errors.Is(err, ErrLinkRefused) {
		t.Fatalf("a playlist gave %v", err)
	}
}

func TestYtdlpOwnBinaryWins(t *testing.T) {
	y, _ := newTestYtdlp(t)
	if got := y.binary(); got != y.Binary {
		t.Fatalf("binary %q, want %q", got, y.Binary)
	}
	own := filepath.Join(y.ConfigDir, "yt-dlp", "yt-dlp")
	if err := os.MkdirAll(filepath.Dir(own), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(own, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := y.binary(); got != own {
		t.Fatalf("binary %q, want the config folder's %q", got, own)
	}
}

func TestYtdlpOwnSaveFolder(t *testing.T) {
	y, root := newTestYtdlp(t)
	own := filepath.Join(t.TempDir(), "videos")
	e := y.Engine("holyarrs", nil, own)
	if _, err := e.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	id, err := e.Add(context.Background(), AddRequest{URL: "https://video.example/watch?v=abc", Category: "holyarrs"})
	if err != nil {
		t.Fatal(err)
	}
	j := waitState(t, e, id, StateFinished)
	if want := filepath.Join(own, "holyarrs", id); j.SavePath != want {
		t.Fatalf("save path %q, want %q (not under %s)", j.SavePath, want, root)
	}
}

func TestYtdlpAddress(t *testing.T) {
	for in, want := range map[string]string{
		"":                          YtdlpURL,
		YtdlpURL:                    YtdlpURL,
		"/data/yt-dlp/":             YtdlpURL + "/data/yt-dlp",
		YtdlpURL + "/data/yt":       YtdlpURL + "/data/yt",
		"/data//downloads/./yt-dlp": YtdlpURL + "/data/downloads/yt-dlp",
	} {
		got, err := ytdlpAddress(in)
		if err != nil || got != want {
			t.Errorf("ytdlpAddress(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"data/yt", "/data/../etc", "http://x/y", "/a?b"} {
		if _, err := ytdlpAddress(bad); !errors.Is(err, ErrYtdlpPath) {
			t.Errorf("ytdlpAddress(%q): %v, want ErrYtdlpPath", bad, err)
		}
	}
	if got := YtdlpFolder(YtdlpURL + "/data/yt"); got != filepath.FromSlash("/data/yt") {
		t.Errorf("folder %q", got)
	}
	if got := YtdlpFolder(YtdlpURL); got != "" {
		t.Errorf("no folder gave %q", got)
	}
}

func TestYtdlpSweepsImportedJobs(t *testing.T) {
	y, _ := newTestYtdlp(t)
	now := time.Now()
	y.Now = func() time.Time { return now }
	e := y.Engine("holyarrs", nil, "")
	ctx := context.Background()
	id, err := e.Add(ctx, AddRequest{URL: "https://video.example/watch?v=abc", Category: "holyarrs"})
	if err != nil {
		t.Fatal(err)
	}
	j := waitState(t, e, id, StateFinished)
	now = now.Add(time.Hour)
	if list, _ := e.List(ctx, ""); len(list) != 1 {
		t.Fatalf("a job whose files are still there must stay: %+v", list)
	}
	// The Import moves the video away; the partial file left behind still holds the job.
	if err := os.Remove(filepath.Join(j.SavePath, j.Files[0].Name)); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.List(ctx, ""); len(list) != 1 {
		t.Fatalf("a leftover partial file must keep the job: %+v", list)
	}
	if err := os.Remove(filepath.Join(j.SavePath, "A video [abc].f1.mp4.part")); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.List(ctx, ""); len(list) != 0 {
		t.Fatalf("an emptied job stays listed: %+v", list)
	}
	if _, err := os.Stat(j.SavePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the emptied job folder is still there: %v", err)
	}
}
