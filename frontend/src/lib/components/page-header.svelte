<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		title,
		description,
		media,
		actions,
		children
	}: {
		title: string;
		description?: string;
		media?: Snippet;
		actions?: Snippet;
		/** Extra content below the title (badges, metadata). */
		children?: Snippet;
	} = $props();
</script>

<header class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
	<div class="flex min-w-0 items-start gap-4">
		{@render media?.()}
		<div class="flex min-w-0 flex-col gap-1">
			<h1 class="truncate text-2xl font-semibold tracking-tight">{title}</h1>
			{#if description}
				<p class="text-sm text-muted-foreground">{description}</p>
			{/if}
			{@render children?.()}
		</div>
	</div>
	{#if actions}
		<div class="flex shrink-0 flex-wrap items-center gap-2">{@render actions()}</div>
	{/if}
</header>
