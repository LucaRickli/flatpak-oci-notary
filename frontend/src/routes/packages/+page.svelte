<script lang="ts">
	import { afterNavigate, goto } from '$app/navigation';
	import { page as appPage } from '$app/state';
	import * as Empty from '$lib/components/ui/empty';
	import * as InputGroup from '$lib/components/ui/input-group';
	import * as Select from '$lib/components/ui/select';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Button } from '$lib/components/ui/button';
	import { Spinner } from '$lib/components/ui/spinner';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { Kbd } from '$lib/components/ui/kbd';
	import PaginatedTable from '$lib/components/paginated-table.svelte';
	import PackagesTable from '$lib/components/packages-table.svelte';
	import PackageCard from '$lib/components/package-card.svelte';
	import SyncUpdatingNote from '$lib/components/sync-updating-note.svelte';
	import { imageClient, RefKind, registryClient } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { onSyncEnded, poll } from '$lib/poll.svelte';
	import { isSyncing, POLL_LIST_MS, syncPollInterval } from '$lib/sync';
	import { canonicalId } from '$lib/ids';
	import { cn } from '$lib/utils/shadcn';
	import { DEFAULT_PAGE_SIZE, maxPage, PAGE_SIZES, pageRequest } from '$lib/pagination';
	import SearchIcon from '@lucide/svelte/icons/search';
	import PackageSearchIcon from '@lucide/svelte/icons/package-search';
	import FunnelXIcon from '@lucide/svelte/icons/funnel-x';
	import XIcon from '@lucide/svelte/icons/x';
	import CpuIcon from '@lucide/svelte/icons/cpu';
	import ServerIcon from '@lucide/svelte/icons/server';
	import LayoutGridIcon from '@lucide/svelte/icons/layout-grid';
	import ListIcon from '@lucide/svelte/icons/list';

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

	// Grid (catalog) or list (table) view, remembered per browser.
	const VIEW_KEY = 'notary.packages.view';
	type View = 'grid' | 'list';
	function readView(): View {
		try {
			return localStorage.getItem(VIEW_KEY) === 'list' ? 'list' : 'grid';
		} catch {
			return 'grid';
		}
	}
	let view = $state<View>(readView());
	function setView(v: string) {
		if (v !== 'grid' && v !== 'list') return;
		view = v;
		try {
			localStorage.setItem(VIEW_KEY, v);
		} catch {
			// Storage unavailable (private mode, blocked): the choice lasts for this page only.
		}
	}

	const multiRegistry = $derived((registries.current?.length ?? 0) > 1);
	let searchInput = $state<HTMLInputElement | null>(null);
	function onkeydown(e: KeyboardEvent) {
		// "/" focuses the search, like on most catalogs.
		const target = e.target as HTMLElement | null;
		if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return;
		if (target?.closest('input, textarea, select, [contenteditable]')) return;
		e.preventDefault();
		searchInput?.focus();
	}
</script>

<svelte:window {onkeydown} />

<section class="flex flex-col gap-4">
	<div class="flex flex-wrap items-end justify-between gap-x-4 gap-y-1">
		<h1 class="text-2xl font-semibold md:text-[1.75rem] md:leading-9">Packages</h1>
		<div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
			{#if syncing}<SyncUpdatingNote />{/if}
			{#if packages.current}
				<span class="tabular-nums">
					{packages.current.totalSize.toLocaleString()}
					{packages.current.totalSize === 1 ? 'package' : 'packages'}{filtered ? ' found' : ''}
				</span>
			{/if}
		</div>
	</div>
	<InputGroup.Root class="h-11 rounded-xl bg-card shadow-xs">
		<InputGroup.Addon class="pl-3.5">
			{#if packages.loading && packages.current}<Spinner />{:else}<SearchIcon />{/if}
		</InputGroup.Addon>
		<InputGroup.Input
			bind:ref={searchInput}
			placeholder="Search apps and runtimes by name, ID or summary"
			bind:value={query}
			aria-label="Search packages"
			class="text-base md:text-sm"
		/>
		<InputGroup.Addon align="inline-end" class="hidden pr-3 sm:flex">
			{#if query}
				<InputGroup.Button size="icon-xs" aria-label="Clear search" onclick={() => (query = '')}><XIcon /></InputGroup.Button>
			{:else}
				<Kbd>/</Kbd>
			{/if}
		</InputGroup.Addon>
	</InputGroup.Root>

	<div class="flex flex-wrap items-center gap-2">
		<ToggleGroup.Root
			type="single"
			variant="segmented"
			spacing={0.5}
			size="sm"
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
			value={architecture || 'all'}
			onValueChange={(v) => {
				architecture = v === 'all' ? '' : v;
				page = 1;
			}}
		>
			<Select.Trigger size="sm" aria-label="Architecture" class={cn(architecture && 'border-primary/50')}>
				<CpuIcon />
				<span class={architecture ? 'font-mono' : ''}>{architecture || 'Any arch'}</span>
			</Select.Trigger>
			<Select.Content>
				<Select.Group>
					<Select.Item value="all">Any architecture</Select.Item>
					{#each architectures as arch (arch)}
						<Select.Item value={arch} class="font-mono">{arch}</Select.Item>
					{/each}
				</Select.Group>
			</Select.Content>
		</Select.Root>
		{#if multiRegistry || registryId}
			<Select.Root
				type="single"
				value={registryId || 'all'}
				onValueChange={(v) => {
					registryId = v === 'all' ? '' : v;
					page = 1;
				}}
			>
				<Select.Trigger size="sm" aria-label="Registry" class={cn('max-w-56', registryId && 'border-primary/50')}>
					<ServerIcon />
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
		{/if}
		{#if filtered}
			<Button variant="ghost" size="sm" onclick={clearFilters}><XIcon data-icon="inline-start" />Reset</Button>
		{/if}
		<ToggleGroup.Root
			type="single"
			variant="segmented"
			spacing={0.5}
			size="sm"
			value={view}
			onValueChange={setView}
			aria-label="View"
			class="ml-auto"
		>
			<ToggleGroup.Item value="grid" aria-label="Grid view" title="Grid view"><LayoutGridIcon /></ToggleGroup.Item>
			<ToggleGroup.Item value="list" aria-label="List view" title="List view"><ListIcon /></ToggleGroup.Item>
		</ToggleGroup.Root>
	</div>
</section>

<PaginatedTable
	result={packages.current}
	loading={packages.loading}
	error={packages.error}
	onretry={packages.refresh}
	bind:page
	bind:pageSize
	skeletonRows={8}
	framed={view === 'list'}
>
	{#snippet children(res)}
		{#if view === 'list'}
			<PackagesTable packages={res.packages} showRegistry={multiRegistry && !registryId} />
		{:else}
			<div class="grid grid-cols-[repeat(auto-fill,minmax(min(100%,17rem),1fr))] gap-3 md:gap-4">
				{#each res.packages as pkg (`${pkg.kind}/${pkg.flatpakId}`)}
					<PackageCard {pkg} showRegistry={multiRegistry && !registryId} />
				{/each}
			</div>
		{/if}
	{/snippet}
	{#snippet skeleton()}
		{#if view === 'grid'}
			<div class="grid grid-cols-[repeat(auto-fill,minmax(min(100%,17rem),1fr))] gap-3 md:gap-4">
				{#each { length: 12 }, i (i)}<Skeleton class="h-36 rounded-xl" />{/each}
			</div>
		{:else}
			<Skeleton class="h-96 rounded-xl" />
		{/if}
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
