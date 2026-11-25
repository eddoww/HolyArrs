package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/downloads"
)

// newYtdlp builds the runner of the built-in yt-dlp Downloader. HOLYARRS_YTDLP
// names the yt-dlp program (default: yt-dlp on the PATH) and
// HOLYARRS_YTDLP_JOBS how many jobs download at once (default 2). A yt-dlp
// binary in <config>/yt-dlp/ wins over both, so a newer one can be dropped in
// when a site changes.
func newYtdlp(configDir string, root func() string) *downloads.Ytdlp {
	jobs, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("HOLYARRS_YTDLP_JOBS")))
	if jobs < 1 || jobs > 16 {
		jobs = 0
	}
	return &downloads.Ytdlp{Root: root, ConfigDir: configDir, Binary: strings.TrimSpace(os.Getenv("HOLYARRS_YTDLP")), MaxJobs: jobs}
}
