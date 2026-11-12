<script lang="ts">
	import { t } from '#lib/core/i18n/i18n.svelte.ts';
	// Your own traits: words, release groups, a size or a list of conditions (a custom format from Sonarr or Radarr) you name once, then give points, make a must-have or put in Never in any Pick rules. A sheet makes or changes one; removing one also takes it out of every Pick rule.
	import SettingsCard from '#lib/ui/SettingsCard.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import LoadError from '#lib/ui/LoadError.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import Sheet from '#lib/ui/Sheet.svelte';
	import Field from '#lib/ui/Field.svelte';
	import SegmentedControl from '#lib/ui/SegmentedControl.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import { blankCondition, checkConditions, checkOwnTrait, checkSizeTrait, conditionKinds, conditionModifiers, conditionResolutions, conditionSources, conditionWords, isPatternKind, languages, ownTerms, sizeParts, sizeTerm } from './pick-rules.ts';
	import type { OwnTrait, TraitCondition } from './types.ts';
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/core/api.ts';
	import { plainMessage as message } from '#lib/core/settings.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';

	let { mayEdit }: { mayEdit: boolean } = $props();

	let list = $state<OwnTrait[] | null>(null);
	let error = $state('');
	let loading = $state(false);

	async function load() {
		loading = true;
		try {
			list = (await api.get<{ own_traits: OwnTrait[] }>('/own-traits')).own_traits;
			error = '';
		} catch (e) {
			error = message(e);
		} finally {
			loading = false;
		}
	}
	onMount(load);

	const matchChoices = $derived<{ value: OwnTrait['match']; label: string }[]>([
		{ value: 'name', label: t('downloads.ot.words') },
		{ value: 'group', label: t('downloads.ot.group') },
		{ value: 'size', label: t('downloads.ot.size') },
		{ value: 'conditions', label: 'Conditions' }
	]);
	// Traits synced from TRaSH Guides sit apart: there can be dozens, and only a sync changes them.
	const handMade = $derived((list ?? []).filter((o) => !o.source));
	const synced = $derived((list ?? []).filter((o) => o.source === 'trash'));
	const specWord = (k: string) => ({ name: t('downloads.ot.kindName'), group: t('downloads.ot.kindGroup'), source: t('downloads.ot.kindSource'), resolution: t('downloads.ot.kindResolution'), size: t('downloads.ot.kindSize') })[k] ?? k;
	const specSummary = (o: OwnTrait) => (o.specs ?? []).map((s) => t(s.negate ? 'downloads.ot.specNot' : 'downloads.ot.spec', { name: s.name, kind: specWord(s.kind) })).join(', ');
	const sizeWords = (term: string) => {
		const { over, upTo } = sizeParts(term);
		return [over && t('downloads.ot.sizeOver', { n: over }), upTo && t('downloads.ot.sizeUpTo', { n: upTo })].filter(Boolean).join(', ');
	};
	const summary = (o: OwnTrait) => (o.match === 'conditions' ? `When: ${(o.conditions ?? []).map(conditionWords).join('; ')}` : o.match === 'size' ? t('downloads.ot.sumSize', { range: sizeWords(o.terms[0] ?? '') }) : t(o.match === 'group' ? 'downloads.ot.sumGroup' : 'downloads.ot.sumName', { terms: o.terms.join(', ') }));

	// The sheet that makes or changes one trait.
	let editing = $state<OwnTrait | null>(null);
	let open = $state(false);
	let name = $state('');
	let match = $state<OwnTrait['match']>('name');
	let termsText = $state('');
	let over = $state('');
	let upTo = $state('');
	let conditions = $state<TraitCondition[]>([]);
	let tried = $state(false);
	let busy = $state(false);
	let serverErrors = $state<Record<string, string>>({});
	let formError = $state('');
	const checked = $derived.by(() => {
		if (match === 'size') return checkSizeTrait(name, over, upTo);
		if (match !== 'conditions') return checkOwnTrait(name, ownTerms(termsText));
		const err = checkOwnTrait(name, ['-']);
		const c = checkConditions(conditions);
		return c ? { ...err, conditions: c } : err;
	});
	const errors = $derived<Record<string, string>>({ ...(tried ? checked : {}), ...serverErrors });

	function openSheet(o: OwnTrait | null) {
		editing = o;
		name = o?.name ?? '';
		match = o?.match ?? 'name';
		termsText = o?.match === 'size' ? '' : (o?.terms.join('\n') ?? '');
		({ over, upTo } = sizeParts(o?.match === 'size' ? o.terms[0] : ''));
		conditions = structuredClone($state.snapshot(o?.conditions ?? [])) as TraitCondition[];
		if (match === 'conditions' && conditions.length === 0) conditions = [blankCondition()];
		tried = false;
		serverErrors = {};
		formError = '';
		open = true;
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		tried = true;
		serverErrors = {};
		formError = '';
		if (Object.keys(checked).length) {
			document.getElementById(checked.name ? 'ot-name' : match === 'size' ? 'ot-over' : checked.conditions ? 'ot-cond-add' : 'ot-terms')?.focus();
			return;
		}
		busy = true;
		const body =
			match === 'conditions'
				? { name: name.trim(), match, terms: [], conditions: conditions.map((c) => ({ ...c, min_gb: c.kind === 'size' ? Number(c.min_gb ?? 0) : undefined, max_gb: c.kind === 'size' ? Number(c.max_gb ?? 0) : undefined })) }
				: { name: name.trim(), match, terms: match === 'size' ? [sizeTerm(over, upTo)] : ownTerms(termsText) };
		try {
			const saved = editing ? await api.put<OwnTrait>(`/own-traits/${editing.id}`, body) : await api.post<OwnTrait>('/own-traits', body);
			const rest = (list ?? []).filter((x) => x.id !== saved.id);
			list = [...rest, saved].sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }));
			open = false;
			toasts.push(editing ? t('downloads.ot.saved', { name: saved.name }) : t('downloads.ot.made', { name: saved.name }), 'ok', editing ? undefined : { body: t('downloads.ot.ready') });
		} catch (err) {
			const field = err instanceof ApiError && typeof err.body.field === 'string' ? err.body.field : '';
			if (field === 'name' || field === 'terms' || field === 'match') serverErrors = { [field]: message(err) };
			else if (field.startsWith('conditions')) serverErrors = { conditions: message(err) };
			else formError = message(err);
		} finally {
			busy = false;
		}
	}

	// Conditions start with one to fill in.
	$effect(() => {
		if (open && match === 'conditions' && conditions.length === 0) conditions = [blankCondition()];
	});

	function setKind(i: number, kind: TraitCondition['kind']) {
		conditions[i] = { ...blankCondition(kind), name: conditions[i].name, negate: conditions[i].negate, required: conditions[i].required };
	}

	// Remove asks first and says what happens to Pick rules that use it.
	let removing = $state<OwnTrait | null>(null);
	let removeOpen = $state(false);
	let removeBusy = $state(false);
	let removeError = $state('');

	async function remove(e: SubmitEvent) {
		e.preventDefault();
		if (!removing) return;
		const gone = removing;
		removeBusy = true;
		removeError = '';
		try {
			const r = await api.del<{ pick_rules_changed: number }>(`/own-traits/${gone.id}`);
			list = (list ?? []).filter((x) => x.id !== gone.id);
			removeOpen = false;
			const n = r?.pick_rules_changed ?? 0;
			toasts.push(t('downloads.ot.removed', { name: gone.name }), 'ok', n ? { body: n === 1 ? t('downloads.ot.outOfOne') : t('downloads.ot.outOfMany', { n }) } : undefined);
		} catch (err) {
			removeError = message(err);
		} finally {
			removeBusy = false;
		}
	}
</script>

<SettingsCard id="own-traits" title={t('downloads.ot.title')} description={t('downloads.ot.desc')}>
	{#if error && !list}
		<LoadError hasData={false} message={error} onretry={load} busy={loading} />
	{:else if list}
		{#if error}<LoadError hasData message={error} onretry={load} busy={loading} />{/if}
		{#if handMade.length === 0}
			<EmptyState compact icon="star" title={t('downloads.ot.none')} text={t('downloads.ot.noneText')} action={mayEdit ? { label: t('downloads.ot.make'), icon: 'plus', onclick: () => openSheet(null) } : undefined} />
		{:else}
			<ul class="rows" data-testid="own-traits">
				{#each handMade as o (o.id)}
					<li data-testid="own-trait">
						<span class="grow">
							<b>{o.name}</b>
							<span class="xs muted meta">{summary(o)}</span>
						</span>
						{#if mayEdit}<span class="acts">
								<Button variant="plain" onclick={() => openSheet(o)}>{t('sources.bridge.change')}</Button>
								<Menu label={t('downloads.ot.moreFor', { name: o.name })} items={[{ label: t('downloads.act.remove'), icon: 'trash', destructive: true, onselect: () => ((removing = o), (removeError = ''), (removeOpen = true)) }]} />
							</span>{/if}
					</li>
				{/each}
			</ul>
			{#if mayEdit}<div><Button icon="plus" onclick={() => openSheet(null)} data-testid="own-trait-add">{t('downloads.ot.make')}</Button></div>{/if}
		{/if}
		{#if synced.length}
			<details class="synced" data-testid="synced-traits">
				<summary>{t('downloads.ot.fromTrash', { n: synced.length })}</summary>
				<p class="xs muted">{t('downloads.ot.syncAgain')}</p>
				<ul class="rows">
					{#each synced as o (o.id)}
						<li data-testid="synced-trait">
							<span class="grow">
								<b>{o.name}</b>
								<span class="xs muted meta">{specSummary(o)}</span>
							</span>
							{#if mayEdit}<span class="acts">
									<Menu label={t('downloads.ot.moreFor', { name: o.name })} items={[{ label: t('downloads.act.remove'), icon: 'trash', destructive: true, onselect: () => ((removing = o), (removeError = ''), (removeOpen = true)) }]} />
								</span>{/if}
						</li>
					{/each}
				</ul>
			</details>
		{/if}
	{/if}
</SettingsCard>

<Sheet bind:open title={editing ? t('downloads.ot.changeTrait') : t('downloads.ot.make')} description={editing?.name} size="sm">
	<form id="own-trait-form" class="form" onsubmit={save} novalidate>
		<Field id="ot-name" label={t('downloads.fact.name')} error={errors.name}>
			<input id="ot-name" class="input" maxlength="60" autocomplete="off" bind:value={name} aria-invalid={errors.name ? 'true' : undefined} aria-describedby={errors.name ? 'ot-name-error' : 'ot-name-hint'} />
		</Field>
		<div class="field">
			<span class="label" id="ot-match-label">{t('downloads.ot.lookAt')}</span>
			<SegmentedControl options={matchChoices} bind:value={match} label={t('downloads.ot.lookAt')} testid="ot-match" />
			<span class="hint">{match === 'size' ? t('downloads.ot.hintSize') : match === 'group' ? t('downloads.ot.hintGroup') : match === 'conditions' ? 'Like a Sonarr or Radarr custom format. Conditions of one kind pass when any of them passes; every kind must pass. A required condition must pass, and Not turns one around.' : t('downloads.ot.hintName')}</span>
		</div>
		{#if match === 'conditions'}
			<fieldset class="conds" aria-describedby={errors.conditions ? 'ot-conditions-error' : undefined}>
				<legend class="label">Conditions</legend>
				<ul data-testid="ot-conditions">
					{#each conditions as c, i (i)}
						<li class="cond">
							<div class="cond-row">
								<label class="sr-only" for="ot-cond-kind-{i}">Kind of condition {i + 1}</label>
								<select id="ot-cond-kind-{i}" class="input" value={c.kind} onchange={(e) => setKind(i, e.currentTarget.value as TraitCondition['kind'])}>
									{#each conditionKinds as [k, l] (k)}<option value={k}>{l}</option>{/each}
								</select>
								<IconButton icon="trash" label="Remove condition {i + 1}" tooltip={false} onclick={() => (conditions = conditions.filter((_, j) => j !== i))} />
							</div>
							{#if isPatternKind(c.kind)}
								<label class="sr-only" for="ot-cond-value-{i}">Pattern</label>
								<input id="ot-cond-value-{i}" class="input mono" spellcheck="false" autocomplete="off" placeholder={'\\bHDR\\b(?!ip)'} bind:value={c.value} />
							{:else if c.kind === 'size'}
								<div class="cond-row">
									<label class="cond-num">Over (GB) <input class="input" type="number" min="0" step="0.1" bind:value={c.min_gb} /></label>
									<label class="cond-num">Up to (GB) <input class="input" type="number" min="0" step="0.1" bind:value={c.max_gb} /></label>
								</div>
							{:else}
								{@const choices = c.kind === 'resolution' ? conditionResolutions : c.kind === 'source' ? conditionSources : c.kind === 'modifier' ? conditionModifiers : [['original', 'The original language'] as [string, string], ...languages]}
								<label class="sr-only" for="ot-cond-value-{i}">Value</label>
								<select id="ot-cond-value-{i}" class="input" bind:value={c.value}>
									{#each choices as [v, l] (v)}<option value={v}>{l}</option>{/each}
								</select>
							{/if}
							<div class="cond-row flags">
								<label class="checkline"><input type="checkbox" bind:checked={c.negate} /> <span>Not</span></label>
								<label class="checkline"><input type="checkbox" bind:checked={c.required} /> <span>Required</span></label>
								{#if c.kind === 'language'}<label class="checkline"><input type="checkbox" bind:checked={c.except} /> <span>Any other language</span></label>{/if}
							</div>
						</li>
					{/each}
				</ul>
				<div><Button id="ot-cond-add" icon="plus" onclick={() => (conditions = [...conditions, blankCondition()])}>Add a condition</Button></div>
				<span class="hint">Patterns are read as Sonarr and Radarr read them, case ignored. One HolyArrs cannot read is refused when you save.</span>
				{#if errors.conditions}<span class="field-error" id="ot-conditions-error" role="alert">{errors.conditions}</span>{/if}
			</fieldset>
		{:else if match === 'size'}
			<div class="pair">
				<Field id="ot-over" label={t('downloads.ot.over')} error={errors.terms}>
					<input id="ot-over" class="input" inputmode="decimal" autocomplete="off" bind:value={over} aria-invalid={errors.terms ? 'true' : undefined} aria-describedby={errors.terms ? 'ot-over-error' : undefined} />
				</Field>
				<Field id="ot-upto" label={t('downloads.ot.upTo')}>
					<input id="ot-upto" class="input" inputmode="decimal" autocomplete="off" bind:value={upTo} />
				</Field>
			</div>
		{:else}
		<Field id="ot-terms" label={match === 'group' ? t('downloads.ot.groups') : t('downloads.ot.wordsLabel')} hint={t('downloads.ot.termsHint')} error={errors.terms}>
			<textarea id="ot-terms" class="input mono" rows="4" spellcheck="false" autocomplete="off" bind:value={termsText} placeholder={match === 'group' ? 'HOLYARRS' : 'NLSubs'} aria-invalid={errors.terms ? 'true' : undefined} aria-describedby={errors.terms ? 'ot-terms-error' : 'ot-terms-hint'}></textarea>
		</Field>
		{/if}
		{#if formError}<p class="form-error" role="alert">{formError}</p>{/if}
	</form>
	{#snippet footer()}
		<Button variant="plain" onclick={() => (open = false)}>{t('downloads.sel.cancel')}</Button>
		<Button variant="primary" type="submit" form="own-trait-form" {busy}>{editing ? t('downloads.tv.save') : t('downloads.ot.makeTrait')}</Button>
	{/snippet}
</Sheet>

<Sheet bind:open={removeOpen} title={t('downloads.ot.removeTitle')} description={removing?.name} size="sm">
	<form id="remove-own-trait" onsubmit={remove}>
		<p class="small">{t('downloads.ot.removeNote')}</p>
		{#if removeError}<p class="form-error" role="alert">{removeError}</p>{/if}
	</form>
	{#snippet footer()}
		<Button variant="plain" onclick={() => (removeOpen = false)}>{t('downloads.sel.cancel')}</Button>
		<Button variant="destructive" type="submit" form="remove-own-trait" busy={removeBusy}>{t('downloads.act.remove')}</Button>
	{/snippet}
</Sheet>

<style>
	.form { display: flex; flex-direction: column; gap: 16px; }
	.pair { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
	.conds { border: 0; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 8px; }
	.conds ul { display: flex; flex-direction: column; gap: 10px; margin: 0; padding: 0; list-style: none; }
	.cond { display: flex; flex-direction: column; gap: 6px; padding: 10px; border-radius: 10px; background: var(--surface-2); }
	.cond-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
	.cond-row select { flex: 1; min-width: 0; }
	.cond-num { display: flex; align-items: center; gap: 6px; font: var(--t-footnote); }
	.cond-num .input { width: 90px; }
	.flags { gap: 4px 16px; }
	.field-error { color: var(--fail-text); font: var(--t-footnote); }
	.synced > summary { cursor: pointer; font-weight: 600; padding: 4px 0; }
	.synced > p { margin: 4px 0 8px; }
	.meta { display: block; overflow-wrap: anywhere; }
	.acts { display: flex; align-items: center; gap: 4px; flex: none; }
	@media (max-width: 520px) {
		.rows > li { flex-wrap: wrap; }
		.acts { width: 100%; justify-content: flex-end; }
	}
</style>
