<script lang="ts">
	import * as Pagination from '$lib/components/ui/pagination';
	import * as Select from '$lib/components/ui/select';
	import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '$lib/pagination';

	let {
		total,
		page = $bindable(1),
		pageSize = $bindable(DEFAULT_PAGE_SIZE),
		pageSizes = PAGE_SIZES,
		onpagechange
	}: {
		/** Number of items across all pages. */
		total: number;
		/** 1-based. */
		page?: number;
		pageSize?: number;
		/** Choices for the page size; empty hides the selector. */
		pageSizes?: number[];
		/** Called when the user navigates to another page. */
		onpagechange?: (page: number) => void;
	} = $props();

	const pageCount = $derived(Math.max(1, Math.ceil(total / pageSize)));
	const first = $derived(Math.min(total, (page - 1) * pageSize + 1));
	const last = $derived(Math.min(total, page * pageSize));

	// Stay in range when the total shrinks, e.g. after a sync or with a stale ?page= in the URL.
	$effect(() => {
		if (page > pageCount) page = pageCount;
	});

	function setPageSize(value: string) {
		const size = Number(value);
		// Keep the first row of the current page visible.
		page = Math.floor(((page - 1) * pageSize) / size) + 1;
		pageSize = size;
	}
</script>

<div class="flex flex-col items-center gap-2 sm:flex-row sm:justify-between">
	<p class="text-sm text-muted-foreground tabular-nums">
		{first.toLocaleString()}–{last.toLocaleString()} of {total.toLocaleString()}
	</p>
	<div class="flex flex-wrap items-center justify-center gap-2">
		{#if pageCount > 1}
			<Pagination.Root count={total} perPage={pageSize} bind:page onPageChange={onpagechange} class="mx-0 w-auto">
				{#snippet children({ pages, currentPage })}
					<Pagination.Content>
						<Pagination.Item><Pagination.Previous /></Pagination.Item>
						{#each pages as item (item.key)}
							<Pagination.Item>
								{#if item.type === 'ellipsis'}
									<Pagination.Ellipsis />
								{:else}
									<Pagination.Link page={item} isActive={currentPage === item.value} />
								{/if}
							</Pagination.Item>
						{/each}
						<Pagination.Item><Pagination.Next /></Pagination.Item>
					</Pagination.Content>
				{/snippet}
			</Pagination.Root>
		{/if}
		{#if pageSizes.length > 1 && total > pageSizes[0]}
			<Select.Root type="single" value={String(pageSize)} onValueChange={setPageSize}>
				<Select.Trigger size="sm" aria-label="Rows per page">{pageSize} / page</Select.Trigger>
				<Select.Content>
					<Select.Group>
						{#each pageSizes as size (size)}
							<Select.Item value={String(size)}>{size} / page</Select.Item>
						{/each}
					</Select.Group>
				</Select.Content>
			</Select.Root>
		{/if}
	</div>
</div>
