<script lang="ts">
	import { afterNavigate, replaceState } from '$app/navigation';
	import { page } from '$app/state';
	import * as Card from '$lib/components/ui/card';
	import * as Empty from '$lib/components/ui/empty';
	import * as InputGroup from '$lib/components/ui/input-group';
	import * as Select from '$lib/components/ui/select';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Button } from '$lib/components/ui/button';
	import { Spinner } from '$lib/components/ui/spinner';
	import PageHeader from '$lib/components/page-header.svelte';
	import ImagesTable from '$lib/components/images-table.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import TableSkeleton from '$lib/components/table-skeleton.svelte';
	import { imageClient, RefKind, registryClient } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import SearchIcon from '@lucide/svelte/icons/search';
	import PackageSearchIcon from '@lucide/svelte/icons/package-search';
	import FunnelXIcon from '@lucide/svelte/icons/funnel-x';

	type KindFilter = 'all' | 'app' | 'runtime';
	const kinds: Record<KindFilter, RefKind> = {
		all: RefKind.UNSPECIFIED,
		app: RefKind.APP,
		runtime: RefKind.RUNTIME
	};

	// Filters live in the URL (?q=&kind=&registry=) so views can be linked and survive reloads.
	function readUrl(url: URL) {
		const k = url.searchParams.get('kind');
		return {
			query: url.searchParams.get('q') ?? '',
			kind: (k === 'app' || k === 'runtime' ? k : 'all') as KindFilter,
			registryId: Number(url.searchParams.get('registry')) || 0
		};
	}

	const initial = readUrl(page.url);
	let query = $state(initial.query);
	let debouncedQuery = $state(initial.query);
	let kind = $state<KindFilter>(initial.kind);
	let registryId = $state(initial.registryId);

	afterNavigate(({ to }) => {
		if (!to) return;
		const f = readUrl(to.url);
		query = debouncedQuery = f.query;
		kind = f.kind;
		registryId = f.registryId;
	});

	$effect(() => {
		const q = query;
		const timer = setTimeout(() => (debouncedQuery = q.trim()), 250);
		return () => clearTimeout(timer);
	});

	$effect(() => {
		const url = new URL(location.href);
		const set = (key: string, value: string) =>
			value ? url.searchParams.set(key, value) : url.searchParams.delete(key);
		set('q', debouncedQuery);
		set('kind', kind === 'all' ? '' : kind);
		set('registry', registryId ? String(registryId) : '');
		if (url.search !== location.search) replaceState(url, {});
	});

	const registries = new Resource(async () => (await registryClient.listRegistries({})).registries);
	const images = new Resource(
		async () =>
			(await imageClient.listImages({ query: debouncedQuery, kind: kinds[kind], registryId })).images
	);

	const filtered = $derived(debouncedQuery !== '' || kind !== 'all' || registryId !== 0);
	const registryLabel = $derived(
		registryId ? (registries.current?.find((r) => r.id === registryId)?.name ?? 'Registry') : 'All registries'
	);

	function clearFilters() {
		query = debouncedQuery = '';
		kind = 'all';
		registryId = 0;
	}
</script>

<PageHeader title="Packages" description="Flatpak images indexed from all registries." />

<div class="flex flex-col gap-3 md:flex-row md:items-center">
	<InputGroup.Root class="md:max-w-sm">
		<InputGroup.Input placeholder="Search name, ref or repository…" bind:value={query} aria-label="Search packages" />
		<InputGroup.Addon>
			{#if images.loading && images.current}<Spinner />{:else}<SearchIcon />{/if}
		</InputGroup.Addon>
	</InputGroup.Root>
	<ToggleGroup.Root
		type="single"
		variant="outline"
		value={kind}
		onValueChange={(v) => v && (kind = v as KindFilter)}
		aria-label="Kind"
	>
		<ToggleGroup.Item value="all">All</ToggleGroup.Item>
		<ToggleGroup.Item value="app">Apps</ToggleGroup.Item>
		<ToggleGroup.Item value="runtime">Runtimes</ToggleGroup.Item>
	</ToggleGroup.Root>
	<Select.Root
		type="single"
		value={String(registryId)}
		onValueChange={(v) => (registryId = Number(v) || 0)}
	>
		<Select.Trigger class="w-full md:w-56" aria-label="Registry">{registryLabel}</Select.Trigger>
		<Select.Content>
			<Select.Group>
				<Select.Item value="0">All registries</Select.Item>
				{#each registries.current ?? [] as r (r.id)}
					<Select.Item value={String(r.id)}>{r.name}</Select.Item>
				{/each}
			</Select.Group>
		</Select.Content>
	</Select.Root>
	{#if images.current}
		<span class="text-sm text-muted-foreground md:ml-auto">
			{images.current.length}
			{images.current.length === 1 ? 'image' : 'images'}
		</span>
	{/if}
</div>

{#if images.error}
	<ErrorAlert error={images.error} onretry={images.refresh} />
{:else if !images.current}
	<Card.Root><Card.Content><TableSkeleton rows={8} /></Card.Content></Card.Root>
{:else if images.current.length === 0}
	<Empty.Root class="border border-dashed">
		{#if filtered}
			<Empty.Header>
				<Empty.Media variant="icon"><FunnelXIcon /></Empty.Media>
				<Empty.Title>No packages match</Empty.Title>
				<Empty.Description>Try a different search or clear the filters.</Empty.Description>
			</Empty.Header>
			<Empty.Content><Button variant="outline" onclick={clearFilters}>Clear filters</Button></Empty.Content>
		{:else if registries.current?.length === 0}
			<Empty.Header>
				<Empty.Media variant="icon"><PackageSearchIcon /></Empty.Media>
				<Empty.Title>No packages yet</Empty.Title>
				<Empty.Description>Add a registry to start indexing flatpak images.</Empty.Description>
			</Empty.Header>
			<Empty.Content><Button href="/registries/new">Add registry</Button></Empty.Content>
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
{:else}
	<Card.Root class="py-0">
		<ImagesTable images={images.current} showRegistry={(registries.current?.length ?? 0) > 1 && !registryId} />
	</Card.Root>
{/if}
