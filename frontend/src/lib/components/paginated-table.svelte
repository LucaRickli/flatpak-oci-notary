<script lang="ts" generics="T extends { totalSize: number }">
	import type { Snippet } from 'svelte';
	import * as Card from '$lib/components/ui/card';
	import TablePagination from './table-pagination.svelte';
	import ErrorAlert from './error-alert.svelte';
	import TableSkeleton from './table-skeleton.svelte';
	import { DEFAULT_PAGE_SIZE } from '$lib/pagination';
	import { cn } from '$lib/utils/shadcn';

	let {
		result,
		loading = false,
		error,
		onretry,
		page = $bindable(1),
		pageSize = $bindable(DEFAULT_PAGE_SIZE),
		skeletonRows = 5,
		framed = true,
		skeleton,
		empty,
		children
	}: {
		/** The loaded page (e.g. a ListPackagesResponse); undefined until the first one arrives. */
		result: T | undefined;
		/** A newer page is loading: the current rows stay visible, dimmed, so the layout doesn't jump. */
		loading?: boolean;
		error?: unknown;
		onretry?: () => void;
		page?: number;
		pageSize?: number;
		skeletonRows?: number;
		/** Wrap the page in a card (tables); false for content that brings its own frame (grids). */
		framed?: boolean;
		/** Replaces the default table skeleton. */
		skeleton?: Snippet;
		/** Shown when nothing matches. */
		empty: Snippet;
		/** The table of the loaded page. */
		children: Snippet<[T]>;
	} = $props();

	let list = $state<HTMLElement>();

	// After navigating from the pagination below the table, jump back to its first row.
	function scrollToTop() {
		if (list && list.getBoundingClientRect().top < 0) list.scrollIntoView({ block: 'start' });
	}
</script>

{#if error}
	<ErrorAlert {error} {onretry} />
{:else if !result}
	{#if skeleton}
		{@render skeleton()}
	{:else}
		<Card.Root><Card.Content><TableSkeleton rows={skeletonRows} /></Card.Content></Card.Root>
	{/if}
{:else if result.totalSize === 0}
	{@render empty()}
{:else}
	<div bind:this={list} class="flex scroll-mt-20 flex-col gap-3">
		{#if framed}
			<Card.Root class={cn('py-0 transition-opacity', loading && 'opacity-60')} aria-busy={loading}>
				{@render children(result)}
			</Card.Root>
		{:else}
			<div class={cn('transition-opacity', loading && 'opacity-60')} aria-busy={loading}>
				{@render children(result)}
			</div>
		{/if}
		<TablePagination total={result.totalSize} bind:page bind:pageSize onpagechange={scrollToTop} />
	</div>
{/if}
