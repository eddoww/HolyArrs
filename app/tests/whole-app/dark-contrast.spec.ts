import { expect, test, type Browser, type Page } from '@playwright/test';
import { axe } from '../helpers/axe.ts';
import { api } from '../helpers/api.ts';
import { ensureInLibrary, openFromSearch, owner, signIn } from '../helpers/helpers.ts';
import { indexerPort, qbitPort } from '../fakes/index.mjs';

// Readable in dark mode, and in light: the pages people set up and use most,
// at desktop and phone width. axe checks the page's text; the browser's own
// parts axe leaves out are checked here too: drop-down rows, placeholders,
// disabled controls, the focus ring, and native controls (date, number,
// drop-down) drawn in the page's theme.

const pages = [
	'/',
	'/explore',
	'/library',
	'/downloads',
	'/downloads/history',
	'/requests',
	'/settings',
	'/settings/account',
	'/settings/appearance',
	'/settings/alerts',
	'/settings/api',
	'/settings/backups',
	'/settings/downloaders',
	'/settings/health',
	'/settings/library-folders',
	'/settings/logs',
	'/settings/media-servers',
	'/settings/move',
	'/settings/music',
	'/settings/naming',
	'/settings/outside-lists',
	'/settings/parts',
	'/settings/pick-rules',
	'/settings/pick-rules/1',
	'/settings/requests',
	'/settings/schedules',
	'/settings/search',
	'/settings/sign-in',
	'/settings/sources',
	'/settings/subtitles',
	'/settings/this-app',
	'/settings/title-defaults',
	'/settings/tmdb',
	'/settings/users'
];

const sizes = { desktop: { width: 1280, height: 800 }, phone: { width: 390, height: 844 } };

/** What a person can add once, so the pages under test have rows to show. */
async function seed(browser: Browser) {
	await ensureInLibrary(browser, 'Driftwood Moon', 'Driftwood Moon');
	const ctx = await browser.newContext();
	const page = await ctx.newPage();
	await signIn(page, owner());
	const call = await api(page);
	const traits = (await (await call('GET', '/own-traits')).json()) as { own_traits: { name: string }[] };
	if (!traits.own_traits.some((t) => t.name === 'Contrast Norwegian')) {
		await call('POST', '/own-traits', { name: 'Contrast Norwegian', match: 'conditions', conditions: [{ name: 'Norwegian', kind: 'language', value: 'no', required: true }, { name: 'Not a cam', kind: 'source', value: 'cam', negate: true }] });
		await call('POST', '/own-traits', { name: 'Contrast huge', match: 'size', terms: ['60-'] });
	}
	const sources = (await (await call('GET', '/sources')).json()) as { sources: { name: string }[] };
	if (!sources.sources.some((s) => s.name === 'Contrast Knaben')) {
		const s = (await (await call('POST', '/sources', { name: 'Contrast Knaben', format: 'knaben', protocol: 'torrent', url: `http://127.0.0.1:${indexerPort}`, kinds: ['movie', 'tv'] })).json()) as { id: number };
		await call('POST', `/sources/${s.id}/pause`, {});
	}
	const dls = (await (await call('GET', '/downloaders')).json()) as { downloaders: { name: string }[] };
	if (!dls.downloaders.some((d) => d.name === 'Contrast qBittorrent')) {
		await call('POST', '/downloaders', { name: 'Contrast qBittorrent', kind: 'qbittorrent', url: `http://127.0.0.1:${qbitPort}`, username: 'admin', secret: 'adminadmin', category: 'radarr', category_shows: 'sonarr', post_import_category: 'radarr-imported', allow_local_network: true });
	}
	const sites = (await (await call('GET', '/subtitle-sites')).json()) as { sites: { name: string }[] };
	if (!sites.sites.some((s) => s.name === 'Contrast OpenSubtitles')) {
		await call('POST', '/subtitle-sites', { name: 'Contrast OpenSubtitles', kind: 'opensubtitles', enabled: false });
	}
	await ctx.close();
}

/**
 * Finds text the browser draws that axe does not measure. Colours are
 * measured over the nearest solid background, with see-through layers mixed
 * in, the way they reach the eye.
 */
async function nativeProblems(page: Page, scheme: 'light' | 'dark'): Promise<string[]> {
	return page.evaluate((scheme) => {
		const out: string[] = [];
		const parse = (c: string): number[] => {
			const m = c.match(/rgba?\(([^)]+)\)/);
			if (!m) return [0, 0, 0, 0];
			const p = m[1].split(/[ ,/]+/).filter(Boolean).map(Number);
			return [p[0], p[1], p[2], p.length > 3 ? p[3] : 1];
		};
		const over = (top: number[], under: number[]) => {
			const a = top[3];
			return [top[0] * a + under[0] * (1 - a), top[1] * a + under[1] * (1 - a), top[2] * a + under[2] * (1 - a), 1];
		};
		const lum = (c: number[]) => {
			const f = (v: number) => {
				v /= 255;
				return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
			};
			return 0.2126 * f(c[0]) + 0.7152 * f(c[1]) + 0.0722 * f(c[2]);
		};
		const ratio = (a: number[], b: number[]) => {
			const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
			return (x + 0.05) / (y + 0.05);
		};
		const page = parse(getComputedStyle(document.body).backgroundColor);
		// The background an element sits on: its own and its ancestors' colours, mixed bottom-up.
		const backdrop = (el: Element | null): number[] => {
			const layers: number[][] = [];
			for (let e = el; e; e = e.parentElement) {
				const c = parse(getComputedStyle(e).backgroundColor);
				if (c[3] > 0) layers.push(c);
				if (c[3] >= 1) break;
			}
			let base = layers.length && layers[layers.length - 1][3] >= 1 ? layers.pop()! : page[3] > 0 ? page : scheme === 'dark' ? [0, 0, 0, 1] : [255, 255, 255, 1];
			for (const l of layers.reverse()) base = over(l, base);
			return base;
		};
		const shown = (el: Element) => {
			const r = el.getBoundingClientRect();
			const st = getComputedStyle(el);
			return r.width > 0 && r.height > 0 && st.visibility !== 'hidden' && st.display !== 'none' && Number(st.opacity) > 0;
		};
		const name = (el: Element) => {
			const id = el.id ? `#${el.id}` : '';
			const label = (el.getAttribute('aria-label') ?? el.getAttribute('placeholder') ?? el.textContent ?? '').trim().slice(0, 40);
			return `${el.tagName.toLowerCase()}${id} "${label}"`;
		};

		for (const sel of Array.from(document.querySelectorAll('select'))) {
			if (!shown(sel)) continue;
			if (getComputedStyle(sel).colorScheme.trim() !== scheme) out.push(`${name(sel)}: its list is drawn in the ${getComputedStyle(sel).colorScheme} scheme`);
			for (const opt of Array.from(sel.querySelectorAll('option, optgroup'))) {
				const st = getComputedStyle(opt);
				const bg = over(parse(st.backgroundColor), backdrop(sel));
				const r = ratio(over(parse(st.color), bg), bg);
				if (r < 4.5) out.push(`${name(sel)} row "${(opt.getAttribute('label') ?? opt.textContent ?? '').trim().slice(0, 30)}": contrast ${r.toFixed(2)}`);
			}
		}
		for (const el of Array.from(document.querySelectorAll('input, textarea'))) {
			const input = el as HTMLInputElement;
			if (!shown(input) || ['checkbox', 'radio', 'range', 'color', 'hidden', 'file'].includes(input.type)) continue;
			if (['date', 'datetime-local', 'time', 'number', 'month', 'week'].includes(input.type) && getComputedStyle(input).colorScheme.trim() !== scheme) {
				out.push(`${name(input)}: its picker is drawn in the ${getComputedStyle(input).colorScheme} scheme`);
			}
			const bg = backdrop(input);
			if (input.placeholder && !input.value) {
				const r = ratio(over(parse(getComputedStyle(input, '::placeholder').color), bg), bg);
				if (r < 4.5) out.push(`${name(input)} placeholder: contrast ${r.toFixed(2)}`);
			}
			if (input.disabled && input.value) {
				const r = ratio(over(parse(getComputedStyle(input).color), bg), bg);
				if (r < 3) out.push(`${name(input)} while off: contrast ${r.toFixed(2)}`);
			}
		}
		for (const b of Array.from(document.querySelectorAll('button:disabled, [aria-disabled="true"]'))) {
			if (!shown(b) || !(b.textContent ?? '').trim()) continue;
			// A control that is off is faded as a whole: its words and its fill both reach the eye mixed with what is under it.
			const st = getComputedStyle(b);
			const under = backdrop(b.parentElement);
			const fill = over(parse(st.backgroundColor), under);
			const op = Number(st.opacity);
			const fg = over([...over(parse(st.color), fill).slice(0, 3), op], under);
			const bg = over([...fill.slice(0, 3), op], under);
			const r = ratio(fg, bg);
			if (r < 3) out.push(`${name(b)} while off: contrast ${r.toFixed(2)}`);
		}
		// The focus ring is the accent around a control: it must stand out from the page and the cards.
		const token = (v: string) => {
			const probe = document.createElement('i');
			probe.style.color = `var(${v})`;
			document.body.append(probe);
			const c = parse(getComputedStyle(probe).color);
			probe.remove();
			return c;
		};
		for (const t of ['--bg', '--surface-1', '--surface-2']) {
			const r = ratio(token('--accent'), token(t));
			if (r < 3) out.push(`focus ring on ${t}: contrast ${r.toFixed(2)}`);
		}
		return out;
	}, scheme);
}

async function check(page: Page, where: string, scheme: 'light' | 'dark') {
	await page.evaluate(() => Promise.all(document.getAnimations().filter((a) => a.effect?.getComputedTiming().iterations !== Infinity).map((a) => a.finished.catch(() => undefined))));
	await page.screenshot({ path: test.info().outputPath(`${where.replace(/[^a-z0-9]+/gi, '_')}.png`), fullPage: true });
	await axe(page, where, { only: ['color-contrast'] });
	expect.soft(await nativeProblems(page, scheme), where).toEqual([]);
}

test.beforeAll(async ({ browser }) => {
	test.setTimeout(120_000);
	await seed(browser);
});

// The rows made for this check go again, so later projects start as before.
test.afterAll(async ({ browser }) => {
	const ctx = await browser.newContext();
	const page = await ctx.newPage();
	await signIn(page, owner());
	const call = await api(page);
	const remove = async (list: string, key: string, path: string) => {
		const rows = ((await (await call('GET', list)).json()) as Record<string, { id?: number; ID?: number; name: string }[]>)[key];
		for (const r of rows.filter((x) => x.name.startsWith('Contrast '))) await call('DELETE', `${path}/${r.id ?? r.ID}`);
	};
	await remove('/own-traits', 'own_traits', '/own-traits');
	await remove('/sources', 'sources', '/sources');
	await remove('/downloaders', 'downloaders', '/downloaders');
	await remove('/subtitle-sites', 'sites', '/subtitle-sites');
	await ctx.close();
});

for (const scheme of ['dark', 'light'] as const) {
	for (const [size, viewport] of Object.entries(sizes)) {
		test(`every page reads well (${scheme}, ${size})`, async ({ browser }) => {
			test.setTimeout(900_000);
			const ctx = await browser.newContext({ colorScheme: scheme, viewport, isMobile: size === 'phone', hasTouch: size === 'phone' });
			await ctx.addInitScript((t) => localStorage.setItem('holyarrs-theme', t), scheme);
			const page = await ctx.newPage();
			await signIn(page, owner());
			await expect(page.locator('html')).toHaveAttribute('data-theme', scheme);
			for (const p of pages) {
				await page.goto(p);
				await page.waitForLoadState('networkidle');
				await check(page, `${p} ${scheme} ${size}`, scheme);
			}

			// A title page from the Library.
			await page.goto('/library');
			await page.locator('a[href^="/library/"]').filter({ hasText: 'Driftwood Moon' }).first().click();
			await page.waitForLoadState('networkidle');
			await check(page, `title page ${scheme} ${size}`, scheme);

			// The trait sheet with conditions, the way a format from Sonarr or Radarr comes over.
			await page.goto('/settings/pick-rules');
			await page.waitForLoadState('networkidle');
			await page.getByTestId('own-trait').filter({ hasText: 'Contrast Norwegian' }).getByRole('button', { name: 'Change' }).click();
			await expect(page.getByTestId('ot-conditions')).toBeVisible();
			await check(page, `trait sheet ${scheme} ${size}`, scheme);
			const sheet = page.getByRole('dialog', { name: 'Change trait' });
			await sheet.getByRole('button', { name: 'Cancel' }).click();
			await expect(sheet).toBeHidden();

			// A site Source's form: the cookie, user agent and freeleech fields.
			await page.goto('/settings/sources');
			await page.waitForLoadState('networkidle');
			const source = page.getByTestId('source').filter({ hasText: 'Contrast Knaben' });
			await source.hover(); // the row's buttons show on hover (always on phones)
			await source.getByRole('button', { name: 'Change', exact: true }).click();
			await check(page, `site source form ${scheme} ${size}`, scheme);

			// The Add sheet of a movie not in the Library, with a second copy (a 4K one, say) and its own drop-downs.
			// Other projects add these; the first one still out of the Library is used.
			let addSheet = false;
			for (const [words, name] of [['ember road', 'Ember Road'], ['cinder waltz', 'Cinder Waltz'], ['northern kettle', 'Northern Kettle']]) {
				await openFromSearch(page, words, name);
				// A Library title opens its Library page instead.
				if (/\/library\/\d+/.test(page.url())) continue;
				await page.getByRole('button', { name: `Add ${name}` }).click();
				const sheet = page.getByRole('dialog', { name: `Add ${name}` });
				await sheet.getByRole('checkbox', { name: /^Also keep a second Version/ }).check();
				await expect(sheet.locator('#more-rules')).toBeVisible();
				await check(page, `add sheet ${scheme} ${size}`, scheme);
				addSheet = true;
				break;
			}
			if (!addSheet) test.info().annotations.push({ type: 'skipped', description: 'every movie for the Add sheet is in the Library already' });

			await ctx.close();
		});
	}
}
