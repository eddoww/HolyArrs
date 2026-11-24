import { describe, expect, it } from 'vitest';
import { addressPlaceholder, emptyForm, formBody, formFrom, seedingFields, YTDLP_URL, type DownloaderView } from './downloaders.ts';
import { linkable } from './link.ts';
import { protocolOf } from './size.ts';
import { clientMark } from './panel-words.ts';
import { pausable } from './usenet.ts';

describe('the yt-dlp Downloader', () => {
	it('sends its save folder as the address and reads it back', () => {
		const f = emptyForm('ytdlp');
		expect(f.url).toBe('');
		expect(formBody(f).url).toBe(YTDLP_URL);
		f.url = ' /data/downloads/yt-dlp ';
		expect(formBody(f).url).toBe('ytdlp://local/data/downloads/yt-dlp');
		const back = formFrom({ ...({} as DownloaderView), kind: 'ytdlp', protocol: 'web', url: 'ytdlp://local/data/yt', path_map: [], tags: [] } as DownloaderView);
		expect(back.url).toBe('/data/yt');
	});

	it('has no seeding choices and no web placeholder address', () => {
		expect(seedingFields('ytdlp')).toBe('none');
		expect(formBody(emptyForm('ytdlp'))).not.toHaveProperty('after_seeding');
		expect(addressPlaceholder('ytdlp')).toBe('/data/downloads/yt-dlp');
		expect(addressPlaceholder('aria2')).toBe('http://192.168.1.10:6800/jsonrpc');
		expect(addressPlaceholder('qbittorrent')).toBe('http://192.168.1.10:8090');
	});

	it('shows its rows as web video that can pause', () => {
		expect(protocolOf({ downloader_kind: 'ytdlp' })).toBe('web');
		expect(protocolOf({ protocol: 'web' })).toBe('web');
		expect(clientMark('ytdlp')).toEqual({ mark: 'yt', tone: 'yt' });
		expect(pausable({ protocol: 'web', phase: 'downloading', downloader_kind: 'ytdlp' })).toBe(true);
	});

	it('takes a link only while nothing is on its way', () => {
		for (const s of ['wanted', 'not_out_yet', 'failed', 'in_library', 'needs_a_decision']) expect(linkable(s)).toBe(true);
		for (const s of ['downloading', 'importing', 'chosen', 'searching', 'stalled', 'removed', undefined]) expect(linkable(s)).toBe(false);
	});
});
