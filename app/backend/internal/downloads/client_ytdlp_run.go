package downloads

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// commandFunc builds one process; tests put a fake yt-dlp in its place.
type commandFunc func(ctx context.Context, name string, args ...string) *exec.Cmd

// execCommand starts yt-dlp so that a stop asks it to end first (it then
// tidies its partial files) and only kills it when it does not.
func execCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, name, args...)
	c.Cancel = func() error {
		if err := c.Process.Signal(os.Interrupt); err != nil {
			return c.Process.Kill()
		}
		return nil
	}
	c.WaitDelay = 15 * time.Second
	return c
}

// ytProgressMark starts each progress line HolyArrs asks yt-dlp for.
const ytProgressMark = "[holyarrs]"

// ytProgressTemplate makes yt-dlp print one easy line per progress step:
// done bytes, total, estimated total, speed and seconds left ("NA" when not known).
const ytProgressTemplate = "download:" + ytProgressMark +
	" %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s %(progress.speed)s %(progress.eta)s"

// own is the person's yt-dlp folder inside the config folder, or "".
func (y *Ytdlp) own(name string) string {
	if y.ConfigDir == "" {
		return ""
	}
	p := filepath.Join(y.ConfigDir, ytdlpFolder, name)
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

// binary is the yt-dlp program to run: the one in the config folder first, so
// a person can drop in a newer one when a site changes, then Binary, then the
// PATH.
func (y *Ytdlp) binary() string {
	if p := y.own("yt-dlp"); p != "" {
		return p
	}
	if y.Binary != "" {
		return y.Binary
	}
	return "yt-dlp"
}

// ffmpeg is the ffmpeg to hand yt-dlp, "" when only the PATH's one (or none)
// is there.
func (y *Ytdlp) ffmpeg() string {
	return y.own("ffmpeg")
}

func (y *Ytdlp) hasFFmpeg() bool {
	if y.ffmpeg() != "" {
		return true
	}
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// version runs yt-dlp --version.
func (y *Ytdlp) version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := y.command(ctx, y.binary(), "--version").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return "", errors.New("yt-dlp was not found: put it on the PATH or in the config folder's yt-dlp folder")
		}
		return "", errors.New("yt-dlp did not start: " + firstLine(err.Error()))
	}
	v := strings.TrimSpace(firstLine(string(out)))
	if v == "" {
		return "", errors.New("yt-dlp gave no version")
	}
	return clipText(v, 60), nil
}

// args are yt-dlp's options for one job.
func (y *Ytdlp) args(j *ytJob) []string {
	a := []string{"--ignore-config"}
	conf := y.own("yt-dlp.conf")
	if conf != "" {
		a = append(a, "--config-locations", conf)
	}
	if c := y.own("cookies.txt"); c != "" {
		a = append(a, "--cookies", c)
	}
	if f := y.ffmpeg(); f != "" {
		a = append(a, "--ffmpeg-location", f)
	}
	if y.hasFFmpeg() {
		a = append(a, "--merge-output-format", "mkv")
	} else if conf == "" {
		a = append(a, "-f", "b")
	}
	a = append(a, y.limitArgs()...)
	return append(a, "--newline", "--no-colors", "--no-playlist", "--continue", "--no-mtime",
		"--progress", "--progress-template", ytProgressTemplate,
		"-P", j.dir, "-o", "%(title).150B [%(id)s].%(ext)s", "--", j.meta.URL)
}

// start runs a job in the background once a slot is free.
func (y *Ytdlp) start(j *ytJob) {
	ctx, cancel := context.WithCancel(context.Background())
	y.mu.Lock()
	if j.cancel != nil {
		y.mu.Unlock()
		cancel()
		return
	}
	j.cancel, j.pausing, j.done = cancel, false, make(chan struct{})
	y.mu.Unlock()
	y.wg.Add(1)
	go func() {
		defer y.wg.Done()
		y.run(ctx, j)
		y.mu.Lock()
		j.cancel = nil
		close(j.done)
		y.mu.Unlock()
		cancel()
	}()
}

// stop ends a running job. With pause the job is kept as paused; without it
// (a remove) the job's state is left alone.
func (y *Ytdlp) stop(j *ytJob, pause bool) {
	y.mu.Lock()
	if j.cancel == nil {
		changed := pause && j.meta.State == StateQueued
		if changed {
			j.meta.State = StatePaused
		}
		y.mu.Unlock()
		if changed {
			_ = writeYtMeta(j)
		}
		return
	}
	j.pausing = pause
	j.cancel()
	done := j.done
	y.mu.Unlock()
	<-done
}

// run waits for a slot and runs yt-dlp for one job.
func (y *Ytdlp) run(ctx context.Context, j *ytJob) {
	select {
	case y.slots <- struct{}{}:
	case <-ctx.Done():
		y.stopped(j)
		return
	}
	defer func() { <-y.slots }()
	y.mu.Lock()
	j.meta.State, j.detail, j.progress = StateDownloading, "", 0
	y.mu.Unlock()
	_ = writeYtMeta(j)

	cmd := y.command(ctx, y.binary(), y.args(j)...)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	lastErr := make(chan string, 1)
	go func() { lastErr <- y.follow(j, pr) }()
	err := cmd.Start()
	if err == nil {
		err = cmd.Wait()
	}
	_ = pw.Close()
	said := <-lastErr

	if ctx.Err() != nil {
		y.stopped(j)
		return
	}
	y.mu.Lock()
	switch {
	case err != nil:
		j.meta.State, j.meta.Error = StateFailed, ytFailure(err, said)
	case len(ytFiles(j.dir)) == 0:
		j.meta.State, j.meta.Error = StateFailed, "yt-dlp ended without leaving a file"
	default:
		j.meta.State, j.meta.Error, j.meta.DoneAt = StateFinished, "", y.now().Unix()
	}
	j.speed, j.eta, j.detail = 0, 0, ""
	y.mu.Unlock()
	_ = writeYtMeta(j)
}

// stopped records a job whose run was cut short: paused when that was asked,
// otherwise left as it was so the next start picks it up again.
func (y *Ytdlp) stopped(j *ytJob) {
	y.mu.Lock()
	pause := j.pausing
	if pause {
		j.meta.State, j.speed, j.eta = StatePaused, 0, 0
	}
	y.mu.Unlock()
	if pause {
		_ = writeYtMeta(j)
	}
}

// follow reads yt-dlp's output into the job's progress and returns the last
// error line it printed.
func (y *Ytdlp) follow(j *ytJob, r io.Reader) string {
	last := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, ytProgressMark):
			y.progress(j, strings.Fields(strings.TrimPrefix(line, ytProgressMark)))
		case strings.HasPrefix(line, "ERROR:"):
			last = strings.TrimSpace(strings.TrimPrefix(line, "ERROR:"))
		case strings.HasPrefix(line, "[Merger]"), strings.HasPrefix(line, "[VideoConvertor]"), strings.HasPrefix(line, "[ExtractAudio]"):
			y.mu.Lock()
			j.detail = "joining"
			y.mu.Unlock()
		}
	}
	_, _ = io.Copy(io.Discard, r)
	return last
}

// progress takes one progress line: done, total, estimated total, speed, eta.
func (y *Ytdlp) progress(j *ytJob, f []string) {
	if len(f) < 5 {
		return
	}
	done, total := ytNum(f[0]), ytNum(f[1])
	if total <= 0 {
		total = ytNum(f[2])
	}
	y.mu.Lock()
	defer y.mu.Unlock()
	if total > 0 {
		j.size = int64(total)
		j.progress = min(1, max(0, done/total))
	}
	j.speed, j.eta = int64(ytNum(f[3])), int64(ytNum(f[4]))
	j.detail = ""
}

func ytNum(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// ytFailure is a short reason for a failed run.
func ytFailure(err error, said string) string {
	if said != "" {
		return clipText(said, 300)
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return "yt-dlp was not found"
	}
	return clipText(firstLine(err.Error()), 300)
}

// ytFiles lists the finished files in a job folder, relative to it. Partial
// files and HolyArrs's own note are left out.
func ytFiles(dir string) []File {
	var out []File
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != dir && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".ytdl") ||
			strings.Contains(name, ".part-Frag") || strings.Contains(name, ".temp.") || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		out = append(out, File{Name: filepath.ToSlash(rel), Size: info.Size(), Progress: 1, Priority: 1})
		return nil
	})
	return out
}

// ytHasMedia says whether a job folder still holds any file other than
// HolyArrs's note, partial files included, so a sweep leaves it alone.
func ytHasMedia(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipAll
		}
		if !d.IsDir() && d.Name() != ytdlpMetaName {
			found = true
		}
		return nil
	})
	return found
}

// writeYtMeta saves what a job folder remembers, through a temporary file so
// a crash never leaves half a note.
func writeYtMeta(j *ytJob) error {
	b, err := json.Marshal(j.meta)
	if err != nil {
		return err
	}
	p := filepath.Join(j.dir, ytdlpMetaName)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return errors.New("yt-dlp's job note could not be written: " + firstLine(err.Error()))
	}
	return os.Rename(tmp, p)
}

// restore reads the jobs left in <root>/yt-dlp and starts again the ones that
// were queued or downloading when HolyArrs stopped.
func (y *Ytdlp) restore(base string) {
	notes, _ := filepath.Glob(filepath.Join(base, "*", "ytdlp-*", ytdlpMetaName))
	for _, p := range notes {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var m ytMeta
		if json.Unmarshal(b, &m) != nil || !ytdlpIDRe.MatchString(m.ID) || filepath.Base(filepath.Dir(p)) != m.ID {
			continue
		}
		j := &ytJob{meta: m, dir: filepath.Dir(p)}
		again := m.State == StateQueued || m.State == StateDownloading
		if again {
			j.meta.State = StateQueued
		}
		if m.State == StateFinished {
			j.progress = 1
		}
		y.mu.Lock()
		_, known := y.jobs[m.ID]
		if !known {
			y.jobs[m.ID] = j
		}
		y.mu.Unlock()
		if again && !known {
			y.start(j)
		}
	}
}

// ytWritable makes the save folder when needed and checks HolyArrs can write
// in it.
func ytWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.New("yt-dlp's save folder could not be made: " + firstLine(err.Error()))
	}
	f, err := os.CreateTemp(dir, ".holyarrs-write-*")
	if err != nil {
		return errors.New("HolyArrs can't write in yt-dlp's save folder " + dir)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

// ytUnder reports whether p is strictly inside base.
func ytUnder(base, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(p))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "..."
}
