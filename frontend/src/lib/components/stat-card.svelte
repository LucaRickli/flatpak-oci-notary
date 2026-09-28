<script lang="ts">
	import type { Component, Snippet } from 'svelte';
	import * as Card from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';

	let {
		label,
		value,
		icon: Icon,
		href,
		children
	}: {
		label: string;
		/** Undefined renders a skeleton. */
		value: string | number | undefined;
		icon?: Component;
		href?: string;
		/** Secondary line below the value. */
		children?: Snippet;
	} = $props();
</script>

<Card.Root size="sm" class="relative">
	<Card.Header>
		<Card.Description class="flex items-center gap-2">
			{#if Icon}<Icon class="size-4" />{/if}
			{#if href}
				<a {href} class="after:absolute after:inset-0 hover:underline">{label}</a>
			{:else}
				{label}
			{/if}
		</Card.Description>
		<Card.Title class="text-2xl font-semibold tabular-nums">
			{#if value === undefined}<Skeleton class="h-8 w-16" />{:else}{value}{/if}
		</Card.Title>
	</Card.Header>
	{#if children}
		<Card.Content class="text-xs text-muted-foreground">{@render children()}</Card.Content>
	{/if}
</Card.Root>
