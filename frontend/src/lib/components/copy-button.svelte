<script lang="ts">
	import { Button, type ButtonProps } from '$lib/components/ui/button';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import CheckIcon from '@lucide/svelte/icons/check';
	import { toast } from 'svelte-sonner';

	let {
		value,
		label = 'Copy',
		showLabel = false,
		variant = 'ghost',
		size = showLabel ? 'sm' : 'icon-sm',
		...restProps
	}: Omit<ButtonProps, 'value'> & { value: string; label?: string; showLabel?: boolean } = $props();

	let copied = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function copy() {
		try {
			await navigator.clipboard.writeText(value);
			copied = true;
			clearTimeout(timer);
			timer = setTimeout(() => (copied = false), 1500);
		} catch {
			toast.error('Could not copy to clipboard');
		}
	}
</script>

<Button {variant} {size} onclick={copy} aria-label={label} title={label} {...restProps}>
	{#if copied}
		<CheckIcon data-icon={showLabel ? 'inline-start' : undefined} />
	{:else}
		<CopyIcon data-icon={showLabel ? 'inline-start' : undefined} />
	{/if}
	{#if showLabel}{copied ? 'Copied' : label}{/if}
</Button>
