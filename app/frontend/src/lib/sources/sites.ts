import { words } from '../core/format.ts';

// The sites HolyArrs reads itself, without Jackett or Prowlarr. The server
// fills in the address, media and newest list a new one leaves out.
export type SiteFormat = 'iptorrents' | 'knaben' | 'subsplease' | 'torrentscsv';

export interface Site {
	label: string;
	url: string;
	// signIn: the site signs in with a browser cookie, typed in the key field.
	signIn: boolean;
	// feed: the site has a newest list.
	feed: boolean;
	kinds: string[];
}

export const sites: Record<SiteFormat, Site> = {
	iptorrents: { label: 'IPTorrents', url: 'https://iptorrents.com/', signIn: true, feed: true, kinds: ['movie', 'tv', 'music'] },
	knaben: { label: 'Knaben', url: 'https://api.knaben.org/', signIn: false, feed: true, kinds: ['movie', 'tv', 'music'] },
	subsplease: { label: 'SubsPlease', url: 'https://subsplease.org/', signIn: false, feed: true, kinds: ['movie', 'tv'] },
	torrentscsv: { label: 'TorrentsCSV', url: 'https://torrents-csv.com/', signIn: false, feed: false, kinds: ['movie', 'tv'] }
};

export function siteOf(format: string): Site | undefined {
	return (sites as Record<string, Site>)[format];
}

// formatLabel names a Source's kind of feed or its site.
export function formatLabel(format: string): string {
	if (format === 'rss') return 'RSS';
	return siteOf(format)?.label ?? words(format);
}
