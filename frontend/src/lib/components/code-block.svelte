<script lang="ts">
	import CopyButton from './copy-button.svelte';
	import { cn } from '$lib/utils/shadcn';

	let {
		code,
		wrap = false,
		class: className
	}: { code: string; wrap?: boolean; class?: string } = $props();
</script>

<div class={cn('relative rounded-lg border bg-muted/50', className)}>
	<pre
		class={cn(
			'overflow-x-auto py-2.5 pr-11 pl-3 font-mono text-xs leading-relaxed',
			wrap ? 'break-all whitespace-pre-wrap' : 'whitespace-pre'
		)}>{code}</pre>
	<CopyButton value={code} class="absolute top-1 right-1" />
</div>
