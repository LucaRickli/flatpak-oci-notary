<script lang="ts">
	import type { Snippet } from 'svelte';
	import ImagesTable from './images-table.svelte';
	import PaginatedTable from './paginated-table.svelte';
	import type { Image } from '$lib/api';
	import { DEFAULT_PAGE_SIZE } from '$lib/pagination';

	let {
		result,
		loading = false,
		error,
		onretry,
		page = $bindable(1),
		pageSize = $bindable(DEFAULT_PAGE_SIZE),
		showRegistry = true,
		missingRuntimes,
		skeletonRows = 5,
		empty
	}: {
		/** The loaded page (e.g. a PreviewRepositoryResponse); undefined until the first one arrives. */
		result: { images: Image[]; totalSize: number } | undefined;
		loading?: boolean;
		error?: unknown;
		onretry?: () => void;
		page?: number;
		pageSize?: number;
		showRegistry?: boolean;
		/** Runtime refs not available to clients, with the flatpak IDs needing them (see ImagesTable). */
		missingRuntimes?: ReadonlyMap<string, ReadonlySet<string>>;
		skeletonRows?: number;
		/** Shown when nothing matches. */
		empty: Snippet;
	} = $props();
</script>

<PaginatedTable {result} {loading} {error} {onretry} bind:page bind:pageSize {skeletonRows} {empty}>
	{#snippet children(r)}
		<ImagesTable images={r.images} {showRegistry} {missingRuntimes} />
	{/snippet}
</PaginatedTable>
