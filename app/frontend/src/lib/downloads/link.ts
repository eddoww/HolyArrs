// Fetching one copy from a web link through the built-in yt-dlp Downloader.

/** Slot states a link may be sent from, the same list the server keeps: nothing is on its way and no search is choosing. */
const linkStates = new Set(['not_out_yet', 'wanted', 'held', 'failed', 'needs_a_decision', 'rejected', 'in_library', 'better_wanted']);

/** Whether a copy in this Slot state can be fetched from a link now. */
export function linkable(state: string | undefined): boolean {
	return !!state && linkStates.has(state);
}
