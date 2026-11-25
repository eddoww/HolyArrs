package downloads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// YtdlpURL is the fixed address of the built-in yt-dlp Downloader: HolyArrs
// runs yt-dlp itself, so there is no host to reach.
const YtdlpURL = "ytdlp://local"

// ytdlpFolder is the folder under the save root that holds every yt-dlp job,
// one folder per category and one per job inside it.
const ytdlpFolder = "yt-dlp"

// ytdlpMetaName is the file in each job folder that remembers the job across
// restarts. It is never listed as one of the job's files.
const ytdlpMetaName = ".holyarrs-ytdlp.json"

var (
	// ErrYtdlpLink means a link handed to yt-dlp is not a plain web address.
	ErrYtdlpLink = errors.New("yt-dlp takes a web address starting with http:// or https://")
	// ErrYtdlpNoRoot means there is no save folder for yt-dlp to write into.
	ErrYtdlpNoRoot = errors.New("yt-dlp needs a save folder: set the save path first")
	ytdlpIDRe      = regexp.MustCompile(`^ytdlp-[0-9a-f]{16}$`)
)

// YtdlpID checks a yt-dlp job id and gives it back unchanged.
func YtdlpID(id string) (string, error) {
	if !ytdlpIDRe.MatchString(id) {
		return "", ErrNotFound
	}
	return id, nil
}

// Ytdlp runs yt-dlp jobs for the whole HolyArrs process. It lives as long as
// the program: every yt-dlp Downloader shares it, and a job keeps running
// while engines are built and dropped around it.
type Ytdlp struct {
	// Root gives the save root; jobs go in <root>/yt-dlp/<category>/<id>.
	Root func() string
	// ConfigDir holds the optional yt-dlp folder: a newer yt-dlp binary, a
	// yt-dlp.conf with the person's own options, and a cookies.txt.
	ConfigDir string
	// Binary is the yt-dlp program to run when the config folder has none;
	// empty means yt-dlp on the PATH.
	Binary string
	// MaxJobs is how many jobs download at once; 0 means 2.
	MaxJobs int
	// Now is the test clock.
	Now func() time.Time
	// command builds the process for one run (tests swap it).
	command commandFunc

	once   sync.Once
	mu     sync.Mutex
	jobs   map[string]*ytJob
	loaded map[string]bool
	slots  chan struct{}
	wg     sync.WaitGroup
	// limit is the speed cap in bytes per second put on each run; 0 is none.
	limit int64
}

// ytMeta is what a job folder remembers about its job.
type ytMeta struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Name     string `json:"name"`
	Category string `json:"category"`
	State    string `json:"state"`
	Error    string `json:"error,omitempty"`
	AddedAt  int64  `json:"added_at"`
	DoneAt   int64  `json:"done_at,omitempty"`
}

// ytJob is one job: what its folder remembers, plus what the running process
// reports.
type ytJob struct {
	meta     ytMeta
	dir      string
	progress float64
	speed    int64
	eta      int64
	size     int64
	detail   string
	cancel   context.CancelFunc
	pausing  bool
	done     chan struct{}
}

func (y *Ytdlp) init() {
	y.once.Do(func() {
		y.jobs = map[string]*ytJob{}
		y.loaded = map[string]bool{}
		n := y.MaxJobs
		if n <= 0 {
			n = 2
		}
		y.slots = make(chan struct{}, n)
		if y.command == nil {
			y.command = execCommand
		}
	})
}

func (y *Ytdlp) now() time.Time {
	if y.Now != nil {
		return y.Now()
	}
	return time.Now()
}

// YtdlpFolder is the save folder a yt-dlp Downloader's address names
// (ytdlp://local/data/yt-dlp), or "" for the save path's yt-dlp folder.
func YtdlpFolder(address string) string {
	p, ok := strings.CutPrefix(address, YtdlpURL)
	if !ok || p == "" || !strings.HasPrefix(p, "/") {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(p))
}

// base is the folder jobs go in: the Downloader's own folder, or
// <root>/yt-dlp. The jobs found there are loaded the first time.
func (y *Ytdlp) base(folder string) (string, error) {
	y.init()
	b := ""
	if folder != "" {
		if !filepath.IsAbs(folder) {
			return "", ErrYtdlpNoRoot
		}
		b = filepath.Clean(folder)
	} else {
		root := ""
		if y.Root != nil {
			root = strings.TrimSpace(y.Root())
		}
		if root == "" || !filepath.IsAbs(root) {
			return "", ErrYtdlpNoRoot
		}
		b = filepath.Join(filepath.Clean(root), ytdlpFolder)
	}
	y.mu.Lock()
	first := !y.loaded[b]
	y.loaded[b] = true
	y.mu.Unlock()
	if first {
		y.restore(b)
	}
	return b, nil
}

// Engine is the yt-dlp engine for one Downloader's categories, saving in
// folder ("" for the save path's yt-dlp folder).
func (y *Ytdlp) Engine(category string, others []string, folder string) *YtdlpEngine {
	y.init()
	return &YtdlpEngine{y: y, Category: category, Others: others, Folder: folder}
}

// Wait stops every running job and waits for them, for a clean exit. A job
// stopped this way keeps its state and goes on at the next start.
func (y *Ytdlp) Wait() {
	y.init()
	y.mu.Lock()
	for _, j := range y.jobs {
		if j.cancel != nil {
			j.cancel()
		}
	}
	y.mu.Unlock()
	y.wg.Wait()
}

// YtdlpEngine is the yt-dlp Downloader seen through the Engine interface.
type YtdlpEngine struct {
	y        *Ytdlp
	Category string
	Others   []string
	// Folder is where jobs go; "" uses the save path's yt-dlp folder.
	Folder string
}

var _ Engine = (*YtdlpEngine)(nil)

func (e *YtdlpEngine) base() (string, error) { return e.y.base(e.Folder) }

// Health runs yt-dlp --version and says whether ffmpeg is there to join
// separate picture and sound.
func (e *YtdlpEngine) Health(ctx context.Context) (Health, error) {
	b, err := e.base()
	if err != nil {
		return Health{}, err
	}
	if err := ytWritable(b); err != nil {
		return Health{}, err
	}
	v, err := e.y.version(ctx)
	if err != nil {
		return Health{}, err
	}
	h := Health{Version: "yt-dlp " + v, At: e.y.now()}
	if !e.y.hasFFmpeg() {
		h.Note = "ffmpeg was not found, so only formats that come as one file can be fetched"
	}
	return h, nil
}

// Add starts a job for the web address in r.URL and returns its id. The same
// address already queued or fetched in the category gives the earlier job.
func (e *YtdlpEngine) Add(ctx context.Context, r AddRequest) (string, error) {
	link, err := ytdlpLink(r.URL)
	if err != nil {
		return "", err
	}
	cat := r.Category
	if cat == "" {
		cat = e.Category
	}
	if _, err := checkCategory(cat, "aria2"); err != nil || cat == "" {
		return "", ErrCategory
	}
	b, err := e.base()
	if err != nil {
		return "", err
	}
	e.y.mu.Lock()
	for _, j := range e.y.jobs {
		if j.meta.URL == link && sameCategory(j.meta.Category, cat) && j.meta.State != StateFailed && !r.Skip[j.meta.ID] {
			id := j.meta.ID
			e.y.mu.Unlock()
			return id, nil
		}
	}
	e.y.mu.Unlock()
	if r.ReconcileOnly {
		return "", errors.Join(ErrUncertain, ErrNotArrived)
	}
	id, err := newYtdlpID()
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(r.Name)
	if name == "" {
		name = link
	}
	j := &ytJob{meta: ytMeta{ID: id, URL: link, Name: name, Category: cat, State: StateQueued, AddedAt: e.y.now().Unix()},
		dir: filepath.Join(b, cat, id)}
	if err := os.MkdirAll(j.dir, 0o755); err != nil {
		return "", errors.New("yt-dlp's folder could not be made: " + firstLine(err.Error()))
	}
	if err := writeYtMeta(j); err != nil {
		return "", err
	}
	e.y.mu.Lock()
	e.y.jobs[id] = j
	e.y.mu.Unlock()
	e.y.start(j)
	return id, nil
}

// List returns the jobs in category; an empty category returns this
// Downloader's every category.
func (e *YtdlpEngine) List(ctx context.Context, category string) ([]Job, error) {
	if _, err := e.base(); err != nil {
		return nil, err
	}
	cats := append([]string{e.Category}, e.Others...)
	if category != "" {
		cats = []string{category}
	}
	e.y.sweep()
	e.y.mu.Lock()
	var picked []*ytJob
	for _, j := range e.y.jobs {
		for _, c := range cats {
			if sameCategory(j.meta.Category, c) {
				picked = append(picked, j)
				break
			}
		}
	}
	out := make([]Job, 0, len(picked))
	for _, j := range picked {
		out = append(out, j.view())
	}
	e.y.mu.Unlock()
	sort.Slice(out, func(a, b int) bool { return out[a].AddedAt < out[b].AddedAt })
	return out, nil
}

// Get returns one job with its files.
func (e *YtdlpEngine) Get(ctx context.Context, id string) (Job, error) {
	if _, err := e.base(); err != nil {
		return Job{}, err
	}
	e.y.mu.Lock()
	j, ok := e.y.jobs[id]
	var v Job
	if ok {
		v = j.view()
	}
	e.y.mu.Unlock()
	if !ok {
		return Job{}, ErrNotFound
	}
	v.Files = ytFiles(v.SavePath)
	if len(v.Files) > 0 && v.State == StateFinished {
		v.Size = 0
	}
	for _, f := range v.Files {
		if v.State != StateFinished {
			break
		}
		v.Size += f.Size
	}
	return v, nil
}

// Remove stops a job and forgets it, deleting its folder when asked. Without
// deleteFiles the fetched files stay where they are.
func (e *YtdlpEngine) Remove(ctx context.Context, id string, deleteFiles bool) error {
	b, err := e.base()
	if err != nil {
		return err
	}
	e.y.mu.Lock()
	j, ok := e.y.jobs[id]
	if ok {
		delete(e.y.jobs, id)
	}
	e.y.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	e.y.stop(j, false)
	if !ytUnder(b, j.dir) {
		return nil
	}
	if deleteFiles {
		return os.RemoveAll(j.dir)
	}
	_ = os.Remove(filepath.Join(j.dir, ytdlpMetaName))
	return nil
}

// PauseItem stops a job's process and keeps its partial files, so a resume
// goes on where it stopped.
func (e *YtdlpEngine) PauseItem(ctx context.Context, id string) error {
	e.y.mu.Lock()
	j, ok := e.y.jobs[id]
	e.y.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	e.y.stop(j, true)
	return nil
}

// ResumeItem starts a paused or failed job again.
func (e *YtdlpEngine) ResumeItem(ctx context.Context, id string) error {
	e.y.mu.Lock()
	j, ok := e.y.jobs[id]
	again := ok && (j.meta.State == StatePaused || j.meta.State == StateFailed)
	if again {
		j.meta.State, j.meta.Error, j.detail = StateQueued, "", ""
	}
	e.y.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	if again {
		_ = writeYtMeta(j)
		e.y.start(j)
	}
	return nil
}

// ytSweepAfter is how long a finished job stays once its files are gone.
const ytSweepAfter = 10 * time.Minute

// sweep forgets finished jobs whose files the Import has moved away, and
// deletes what is left of their folders (the job note, empty folders). A job
// that still holds a file is kept, so nothing an Import could want is lost.
func (y *Ytdlp) sweep() {
	cut := y.now().Add(-ytSweepAfter).Unix()
	y.mu.Lock()
	var done []*ytJob
	for _, j := range y.jobs {
		if j.meta.State == StateFinished && j.meta.DoneAt > 0 && j.meta.DoneAt < cut && j.cancel == nil {
			done = append(done, j)
		}
	}
	y.mu.Unlock()
	for _, j := range done {
		if len(ytFiles(j.dir)) > 0 || ytHasMedia(j.dir) {
			continue
		}
		y.mu.Lock()
		delete(y.jobs, j.meta.ID)
		y.mu.Unlock()
		if ytdlpIDRe.MatchString(filepath.Base(j.dir)) {
			_ = os.RemoveAll(j.dir)
		}
	}
}

// view is the job in the Engine's words; the caller holds y.mu.
func (j *ytJob) view() Job {
	v := Job{ID: j.meta.ID, Name: j.meta.Name, Category: j.meta.Category, SavePath: j.dir, ContentPath: j.dir,
		State: j.meta.State, Detail: j.detail, AddedAt: j.meta.AddedAt, DoneAt: j.meta.DoneAt,
		Progress: j.progress, Speed: j.speed, ETA: j.eta, Size: j.size}
	switch j.meta.State {
	case StateFinished:
		v.Progress, v.Speed, v.ETA = 1, 0, 0
	case StateFailed:
		v.Detail = "error"
		if j.meta.Error != "" {
			v.Detail = "error: " + j.meta.Error
		}
	}
	return v
}

// ytdlpLink checks a link for yt-dlp: http or https with a host, no sign-in
// in it.
func ytdlpLink(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || len(raw) > 2000 {
		return "", ErrYtdlpLink
	}
	return u.String(), nil
}

func newYtdlpID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ytdlp-" + hex.EncodeToString(b), nil
}
