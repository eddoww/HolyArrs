<script lang="ts">
	// Fetch one copy from a web link: the built-in yt-dlp Downloader takes any
	// site yt-dlp reads, and the file is imported like any other download.
	import Button from '#lib/ui/Button.svelte';
	import { api, message } from '#lib/core/api.ts';
	import { toasts } from '#lib/core/toasts.svelte.ts';
	import { t } from '#lib/core/i18n/i18n.svelte.ts';

	let { titleId, onsent }: { titleId: number; onsent?: () => void | Promise<void> } = $props();

	let dialog: HTMLDialogElement | undefined = $state();
	let slotId = $state(0);
	let label = $state('');
	let link = $state('');
	let busy = $state(false);
	let error = $state('');

	/** Opens the dialog for one copy; label names it ("Main", "S01E02"). */
	export function open(slot: number, name: string) {
		slotId = slot;
		label = name;
		link = '';
		error = '';
		dialog?.showModal();
	}

	const linkOk = (s: string) => /^https?:\/\/[^\s/]+/i.test(s.trim());

	async function send(e: SubmitEvent) {
		e.preventDefault();
		if (!linkOk(link)) {
			error = t('downloads.link.badLink');
			return;
		}
		busy = true;
		error = '';
		try {
			const r = await api.post<{ title: string }>(`/titles/${titleId}/link`, { slot_id: slotId, url: link.trim() });
			dialog?.close();
			toasts.push(t('downloads.link.sent', { name: r.title || link.trim() }), 'info');
			await onsent?.();
		} catch (err) {
			error = message(err);
		} finally {
			busy = false;
		}
	}
</script>

<dialog bind:this={dialog} aria-labelledby="link-fetch-title" class="modal" data-testid="link-dialog">
	<form onsubmit={send}>
		<h2 id="link-fetch-title" class="h2">{t('downloads.link.title')}</h2>
		<p class="small muted">{t('downloads.link.intro', { label })}</p>
		<div class="field" style="margin-top: var(--s-5)">
			<label for="link-fetch-url">{t('downloads.link.address')}</label>
			<input id="link-fetch-url" class="input mono" type="url" bind:value={link} placeholder="https://" autocomplete="off" spellcheck="false" required data-testid="link-url" />
			<span class="hint">{t('downloads.link.hint')}</span>
		</div>
		{#if error}<p class="form-error" role="alert">{error}</p>{/if}
		<div class="row modal-actions">
			<Button variant="plain" onclick={() => dialog?.close()}>{t('downloads.link.cancel')}</Button>
			<Button variant="primary" type="submit" disabled={busy || !link.trim()} {busy}>{t('downloads.link.fetch')}</Button>
		</div>
	</form>
</dialog>

<style>
	.modal { width: min(480px, calc(100vw - 32px)); max-height: calc(100dvh - 32px); overflow: auto; padding: var(--s-6); border: 0; border-radius: var(--r-modal); background: var(--surface-1); color: var(--text); box-shadow: var(--shadow-modal); }
	.modal::backdrop { background: var(--scrim); }
	.modal-actions { justify-content: flex-end; margin-top: var(--s-6); }
</style>
