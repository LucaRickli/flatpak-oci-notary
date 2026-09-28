<script lang="ts">
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { Button } from '$lib/components/ui/button';
	import { Spinner } from '$lib/components/ui/spinner';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';

	let {
		title,
		description,
		label = 'Delete',
		onconfirm
	}: {
		title: string;
		description: string;
		label?: string;
		/** Performs the deletion; return false to keep the dialog open (e.g. on error). */
		onconfirm: () => Promise<boolean | void>;
	} = $props();

	let open = $state(false);
	let busy = $state(false);

	async function confirm() {
		busy = true;
		try {
			if ((await onconfirm()) !== false) open = false;
		} finally {
			busy = false;
		}
	}
</script>

<AlertDialog.Root bind:open>
	<AlertDialog.Trigger>
		{#snippet child({ props })}
			<Button variant="destructive" {...props}><Trash2Icon data-icon="inline-start" />{label}</Button>
		{/snippet}
	</AlertDialog.Trigger>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{title}</AlertDialog.Title>
			<AlertDialog.Description>{description}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel disabled={busy}>Cancel</AlertDialog.Cancel>
			<Button variant="destructive" disabled={busy} onclick={confirm}>
				{#if busy}<Spinner data-icon="inline-start" />{/if}
				{label}
			</Button>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
