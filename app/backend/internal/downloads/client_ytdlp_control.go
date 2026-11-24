package downloads

import (
	"context"
	"strconv"
)

var _ Controller = (*YtdlpEngine)(nil)

// own lists this Downloader's jobs (its categories), for the controls.
func (e *YtdlpEngine) own() []*ytJob {
	cats := append([]string{e.Category}, e.Others...)
	e.y.mu.Lock()
	defer e.y.mu.Unlock()
	var out []*ytJob
	for _, j := range e.y.jobs {
		for _, c := range cats {
			if sameCategory(j.meta.Category, c) {
				out = append(out, j)
				break
			}
		}
	}
	return out
}

// PauseAll pauses every queued or running job of this Downloader.
func (e *YtdlpEngine) PauseAll(ctx context.Context) error {
	for _, j := range e.own() {
		e.y.mu.Lock()
		live := j.meta.State == StateQueued || j.meta.State == StateDownloading
		e.y.mu.Unlock()
		if live {
			e.y.stop(j, true)
		}
	}
	return nil
}

// ResumeAll starts every paused job of this Downloader again.
func (e *YtdlpEngine) ResumeAll(ctx context.Context) error {
	for _, j := range e.own() {
		e.y.mu.Lock()
		paused := j.meta.State == StatePaused
		e.y.mu.Unlock()
		if paused {
			if err := e.ResumeItem(ctx, j.meta.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// MoveTop has no meaning here: jobs start in the order they came.
func (e *YtdlpEngine) MoveTop(context.Context, string) error { return ErrNotSupported }

// SetSpeedLimit caps each yt-dlp run's download speed in bytes per second; 0
// removes the cap. It holds from the next run on, so a running video keeps
// its speed until it is paused and resumed.
func (e *YtdlpEngine) SetSpeedLimit(ctx context.Context, down, up int64) error {
	e.y.mu.Lock()
	e.y.limit = max(0, down)
	e.y.mu.Unlock()
	return nil
}

// Status adds up the speed of this Downloader's running jobs.
func (e *YtdlpEngine) Status(ctx context.Context) (ClientStatus, error) {
	var st ClientStatus
	live, paused := false, false
	for _, j := range e.own() {
		e.y.mu.Lock()
		st.Speed += j.speed
		live = live || j.meta.State == StateQueued || j.meta.State == StateDownloading
		paused = paused || j.meta.State == StatePaused
		e.y.mu.Unlock()
	}
	e.y.mu.Lock()
	st.SpeedLimit = e.y.limit
	e.y.mu.Unlock()
	// Paused as a whole: something waits paused and nothing runs.
	st.Paused = paused && !live
	return st, nil
}

// answeredWord names what answered a connection test: the adapter and its
// version, said once when the version already names the program (yt-dlp).
func answeredWord(adapter, version string) string {
	if adapter == "ytdlp" {
		return version
	}
	return adapter + " " + version
}

// limitArgs is yt-dlp's speed cap for a run, none when no cap is set.
func (y *Ytdlp) limitArgs() []string {
	y.mu.Lock()
	n := y.limit
	y.mu.Unlock()
	if n <= 0 {
		return nil
	}
	return []string{"--limit-rate", strconv.FormatInt(n, 10)}
}
