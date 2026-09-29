<script lang="ts">
	import { afterNavigate, goto } from '$app/navigation';
	import { page as appPage } from '$app/state';
	import * as Empty from '$lib/components/ui/empty';
	import * as InputGroup from '$lib/components/ui/input-group';
	import * as Select from '$lib/components/ui/select';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Button } from '$lib/components/ui/button';
	import { Spinner } from '$lib/components/ui/spinner';
	import PageHeader from '$lib/components/page-header.svelte';
	import PaginatedTable from '$lib/components/paginated-table.svelte';
	import PackagesTable from '$lib/components/packages-table.svelte';
	import SyncUpdatingNote from '$lib/components/sync-updating-note.svelte';
	import { imageClient, RefKind, registryClient } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { onSyncEnded, poll } from '$lib/poll.svelte';
	import { isSyncing, POLL_LIST_MS, syncPollInterval } from '$lib/sync';
	import { canonicalId } from '$lib/ids';
	import { DEFAULT_PAGE_SIZE, maxPage, PAGE_SIZES, pageRequest } from '$lib/pagination';
	import SearchIcon from '@lucide/svelte/icons/search';
	import PackageSearchIcon from '@lucide/svelte/icons/package-search';
	import FunnelXIcon from '@lucide/svelte/icons/funnel-x';

	type KindFilter = 'all' | 'app' | 'runtime';
	const kinds: Record<KindFilter, RefKind> = {
		all: RefKind.UNSPECIFIED,
		app: RefKind.APP,
		runtime: RefKind.RUNTIME
	};

	// Common OCI architectures of flatpak images (x86_64, aarch64, i386, arm, ...).
	const architectures = ['amd64', 'arm64', '386', 'arm', 'ppc64le', 's390x', 'riscv64'];

	// Filters and pagination live in the URL (?q=&kind=&registry=&arch=&page=&size=) so views can be
	// linked and survive reloads.
	function readUrl(url: URL) {
		const params = url.searchParams;
		const k = params.get('kind');
		const registry = params.get('registry');
		const size = Number(params.get('size'));
		const pageSize = PAGE_SIZES.includes(size) ? size : DEFAULT_PAGE_SIZE;
		// Capped so the offset fits the API (a huge ?page= would otherwise fail
		// to encode); an out-of-range page is then moved back by the pagination.
		const page = Math.min(
			Math.max(1, Math.trunc(Number(params.get('page'))) || 1),
			maxPage(pageSize)
		);
		return {
			query: (params.get('q') ?? '').trim(),
			kind: (k === 'app' || k === 'runtime' ? k : 'all') as KindFilter,
			registryId: canonicalId(registry) ?? '',
			architecture: params.get('arch') ?? '',
			page,
			pageSize
		};
	}

	const initial = readUrl(appPage.url);
	let query = $state(initial.query);
	let debouncedQuery = $state(initial.query);
	let kind = $state<KindFilter>(initial.kind);
	let registryId = $state(initial.registryId);
	let architecture = $state(initial.architecture);
	let page = $state(initial.page);
	let pageSize = $state(initial.pageSize);

	afterNavigate(({ to }) => {
		if (!to) return;
		const f = readUrl(to.url);
		// A search typed while the URL update for the previous one was in flight is kept.
		if (f.query !== debouncedQuery) query = f.query;
		debouncedQuery = f.query;
		kind = f.kind;
		registryId = f.registryId;
		architecture = f.architecture;
		page = f.page;
		pageSize = f.pageSize;
	});

	// Debounced search. Like every filter change, a new search goes back to the first page.
	$effect(() => {
		const q = query.trim();
		const timer = setTimeout(() => {
			if (q === debouncedQuery) return;
			debouncedQuery = q;
			page = 1;
		}, 250);
		return () => clearTimeout(timer);
	});

	$effect(() => {
		const url = new URL(location.href);
		const set = (key: string, value: string) =>
			value ? url.searchParams.set(key, value) : url.searchParams.delete(key);
		set('q', debouncedQuery);
		set('kind', kind === 'all' ? '' : kind);
		set('registry', registryId);
		set('arch', architecture);
		set('page', page > 1 ? String(page) : '');
		set('size', pageSize !== DEFAULT_PAGE_SIZE ? String(pageSize) : '');
		// A real (not shallow) navigation, so page.url and the history entry carry the filters and
		// Back to this page restores them. The load only depends on the route, so nothing reloads.
		if (url.search !== location.search) goto(url, { replaceState: true, keepFocus: true, noScroll: true });
	});

	const registries = new Resource(async () => (await registryClient.listRegistries({})).registries);
	const packages = new Resource(() =>
		imageClient.listPackages({
			query: debouncedQuery,
			kind: kinds[kind],
			registryId,
			architecture,
			...pageRequest(page, pageSize)
		})
	);

	// While a (relevant) registry syncs, its indexed repositories show up as they're done: refresh
	// the list in the background, and once more when the sync has ended (also one that ran between
	// two polls of the registry state).
	const syncing = $derived(
		registries.current?.some((r) => isSyncing(r) && (!registryId || r.id === registryId)) ?? false
	);
	poll(() => syncPollInterval(registries.current), registries.poll);
	poll(() => (syncing ? POLL_LIST_MS : undefined), packages.poll);
	onSyncEnded(
		() => registries.current,
		(r) => {
			if (!registryId || r.id === registryId) packages.poll();
		}
	);

	const filtered = $derived(debouncedQuery !== '' || kind !== 'all' || registryId !== '' || architecture !== '');
	const registryLabel = $derived(
		registryId ? (registries.current?.find((r) => r.id === registryId)?.name ?? 'Registry') : 'All registries'
	);

	function clearFilters() {
		query = debouncedQuery = '';
		kind = 'all';
		registryId = '';
		architecture = '';
		page = 1;
	}
</script>

<PageHeader
	title="Packages"
	description="Flatpak apps and runtimes indexed from all registries, across architectures, branches and tags."
/>

<div class="flex flex-col gap-3 md:flex-row md:flex-wrap md:items-center">
	<InputGroup.Root class="md:max-w-sm">
		<InputGroup.Input placeholder="Search name, ID or summary…" bind:value={query} aria-label="Search packages" />
		<InputGroup.Addon>
			{#if packages.loading && packages.current}<Spinner />{:else}<SearchIcon />{/if}
		</InputGroup.Addon>
	</InputGroup.Root>
	<ToggleGroup.Root
		type="single"
		variant="outline"
		value={kind}
		onValueChange={(v) => {
			if (!v) return;
			kind = v as KindFilter;
			page = 1;
		}}
		aria-label="Kind"
	>
		<ToggleGroup.Item value="all">All</ToggleGroup.Item>
		<ToggleGroup.Item value="app">Apps</ToggleGroup.Item>
		<ToggleGroup.Item value="runtime">Runtimes</ToggleGroup.Item>
	</ToggleGroup.Root>
	<Select.Root
		type="single"
		value={registryId || 'all'}
		onValueChange={(v) => {
			registryId = v === 'all' ? '' : v;
			page = 1;
		}}
	>
		<Select.Trigger class="w-full md:w-56" aria-label="Registry">
			<span class="truncate">{registryLabel}</span>
		</Select.Trigger>
		<Select.Content>
			<Select.Group>
				<Select.Item value="all">All registries</Select.Item>
				{#each registries.current ?? [] as r (r.id)}
					<Select.Item value={r.id}>{r.name}</Select.Item>
				{/each}
			</Select.Group>
		</Select.Content>
	</Select.Root>
	<Select.Root
		type="single"
		value={architecture || 'all'}
		onValueChange={(v) => {
			architecture = v === 'all' ? '' : v;
			page = 1;
		}}
	>
		<Select.Trigger class="w-full md:w-44" aria-label="Architecture">
			{architecture || 'All architectures'}
		</Select.Trigger>
		<Select.Content>
			<Select.Group>
				<Select.Item value="all">All architectures</Select.Item>
				{#each architectures as arch (arch)}
					<Select.Item value={arch} class="font-mono">{arch}</Select.Item>
				{/each}
			</Select.Group>
		</Select.Content>
	</Select.Root>
	{#if packages.current || syncing}
		<div class="flex flex-wrap items-center gap-x-4 gap-y-1 md:ml-auto">
			{#if syncing}<SyncUpdatingNote />{/if}
			{#if packages.current}
				<span class="text-sm text-muted-foreground tabular-nums">
					{packages.current.totalSize.toLocaleString()}
					{packages.current.totalSize === 1 ? 'package' : 'packages'}
				</span>
			{/if}
		</div>
	{/if}
</div>

<PaginatedTable
	result={packages.current}
	loading={packages.loading}
	error={packages.error}
	onretry={packages.refresh}
	bind:page
	bind:pageSize
	skeletonRows={8}
>
	{#snippet children(res)}
		<PackagesTable packages={res.packages} showRegistry={(registries.current?.length ?? 0) > 1 && !registryId} />
	{/snippet}
	{#snippet empty()}
		<Empty.Root class="border border-dashed">
			{#if filtered}
				<Empty.Header>
					<Empty.Media variant="icon"><FunnelXIcon /></Empty.Media>
					<Empty.Title>No packages match</Empty.Title>
					<Empty.Description>
						Try a different search or clear the filters.{syncing ? ' A sync is still running.' : ''}
					</Empty.Description>
				</Empty.Header>
				<Empty.Content><Button variant="outline" onclick={clearFilters}>Clear filters</Button></Empty.Content>
			{:else if registries.current?.length === 0}
				<Empty.Header>
					<Empty.Media variant="icon"><PackageSearchIcon /></Empty.Media>
					<Empty.Title>No packages yet</Empty.Title>
					<Empty.Description>Add a registry to start indexing flatpak images.</Empty.Description>
				</Empty.Header>
				<Empty.Content><Button href="/registries/new">Add registry</Button></Empty.Content>
			{:else if syncing}
				<Empty.Header>
					<Empty.Media variant="icon"><Spinner /></Empty.Media>
					<Empty.Title>Indexing…</Empty.Title>
					<Empty.Description>Packages show up here as the sync indexes their repositories.</Empty.Description>
				</Empty.Header>
			{:else}
				<Empty.Header>
					<Empty.Media variant="icon"><PackageSearchIcon /></Empty.Media>
					<Empty.Title>No packages indexed</Empty.Title>
					<Empty.Description>
						Sync a registry to index its images. Only images with an org.flatpak.ref label show up here.
					</Empty.Description>
				</Empty.Header>
				<Empty.Content><Button href="/registries" variant="outline">Go to registries</Button></Empty.Content>
			{/if}
		</Empty.Root>
	{/snippet}
</PaginatedTable>
