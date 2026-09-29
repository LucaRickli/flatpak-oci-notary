<script lang="ts">
	import { afterNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import { tick } from 'svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Empty from '$lib/components/ui/empty';
	import * as Alert from '$lib/components/ui/alert';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import PageHeader from '$lib/components/page-header.svelte';
	import PackageIcon from '$lib/components/package-icon.svelte';
	import KindBadge from '$lib/components/kind-badge.svelte';
	import CopyButton from '$lib/components/copy-button.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import VariantsTable from '$lib/components/variants-table.svelte';
	import ImageDetails from '$lib/components/image-details.svelte';
	import ExtraDataBadge from '$lib/components/metadata/extra-data-badge.svelte';
	import { imageClient, type Image } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { isNotFound } from '$lib/session.svelte';
	import { setCrumb } from '$lib/breadcrumb.svelte';
	import { packageTitle, toDate } from '$lib/format';
	import { VARIANT_PARAM } from '$lib/links';
	import { canonicalId } from '$lib/ids';
	import { cn } from '$lib/utils/shadcn';
	import ServerIcon from '@lucide/svelte/icons/server';
	import InfoIcon from '@lucide/svelte/icons/info';

	let { data } = $props();

	const resource = new Resource(() => imageClient.getPackage({ kind: data.kind, flatpakId: data.flatpakId }));
	// Ignore the previously viewed package while the next one loads.
	const current = $derived.by(() => {
		const p = resource.current?.package;
		return p && p.kind === data.kind && p.flatpakId === data.flatpakId ? resource.current : undefined;
	});
	const pkg = $derived(current?.package);
	const variants = $derived(current?.variants ?? []);

	// Set when a linked variant is no longer indexed (see below).
	let staleVariant = $state<{ requested: string; shown: string } | undefined>();

	// The selected variant is kept in ?variant=<image id> (links to a specific image land here).
	const variantParam = (url: URL) => canonicalId(url.searchParams.get(VARIANT_PARAM)) ?? '';
	let requested = $state(variantParam(page.url));
	afterNavigate(({ from, to }) => {
		if (!to) return;
		requested = variantParam(to.url);
		// Another package: forget the notice about a stale link to this one.
		if (from?.url.pathname !== to.url.pathname) staleVariant = undefined;
	});

	const isAmd64 = (v: Image) => v.architecture === 'amd64' || v.arch === 'x86_64';
	const time = (v: Image) => toDate(v.created ?? v.indexedAt)?.getTime() ?? 0;

	/** Newest amd64 image on the stable branch, falling back to less specific matches. */
	function defaultVariant(list: Image[]): Image | undefined {
		const tiers = [(v: Image) => isAmd64(v) && v.branch === 'stable', isAmd64, (v: Image) => v.branch === 'stable'];
		const pool = tiers.map((t) => list.filter(t)).find((l) => l.length > 0) ?? list;
		return pool.reduce<Image | undefined>((best, v) => (!best || time(v) > time(best) ? v : best), undefined);
	}

	const selected = $derived(variants.find((v) => v.id === requested) ?? defaultVariant(variants));
	// A string, so refreshed variant objects with the same id don't reload the details.
	const selectedId = $derived(selected?.id);

	/**
	 * Keeps the selection in the URL. A real (not shallow) navigation, so page.url and the history
	 * entry carry it and Back to this page restores it; the load only depends on the route, so
	 * nothing reloads. Without an id the parameter is dropped (the default variant is shown).
	 */
	function setVariantParam(id: string | undefined) {
		const url = new URL(location.href);
		if (id) url.searchParams.set(VARIANT_PARAM, id);
		else url.searchParams.delete(VARIANT_PARAM);
		if (url.search !== location.search) return goto(url, { replaceState: true, keepFocus: true, noScroll: true });
	}

	// A linked variant that is no longer indexed (removed by a sync): say so instead of silently
	// showing another one, and drop the stale id from the URL.
	$effect(() => {
		if (!current || variants.length === 0 || !requested || variants.some((v) => v.id === requested)) return;
		const shown = defaultVariant(variants);
		staleVariant = { requested, shown: shown ? `${shown.arch || shown.architecture}/${shown.branch}` : '' };
		requested = '';
		setVariantParam(undefined);
	});

	let detailsSection = $state<HTMLElement>();

	async function select(variant: Image) {
		requested = variant.id;
		staleVariant = undefined;
		await setVariantParam(variant.id);
		await tick();
		// On small screens the details are far below the table: bring them into view.
		if (detailsSection && detailsSection.getBoundingClientRect().top > window.innerHeight) {
			detailsSection.scrollIntoView({ behavior: 'smooth', block: 'start' });
		}
	}

	const detail = new Resource(async () => {
		const id = selectedId;
		return id ? imageClient.getImage({ id }) : undefined;
	});
	// While another variant loads, keep showing the previous one of this package (dimmed).
	const shown = $derived.by(() => {
		const d = detail.current;
		const image = d?.image;
		return d && image && variants.some((v) => v.id === image.id) ? { ...d, image } : undefined;
	});
	const detailStale = $derived(!!shown && shown.image.id !== selectedId);

	const architectures = $derived(pkg?.architectures ?? []);
	const branches = $derived(pkg?.branches ?? []);

	$effect(() => {
		if (pkg) setCrumb(packageTitle(pkg));
	});
</script>

{#if resource.error}
	{#if isNotFound(resource.error)}
		<Empty.Root>
			<Empty.Header>
				<Empty.Title>Package not found</Empty.Title>
				<Empty.Description>
					No indexed image provides {data.flatpakId}. It may have been removed from its registry during a sync.
				</Empty.Description>
			</Empty.Header>
			<Empty.Content><Button href="/packages" variant="outline">Back to packages</Button></Empty.Content>
		</Empty.Root>
	{:else}
		<ErrorAlert error={resource.error} onretry={resource.refresh} />
	{/if}
{:else if !current || !pkg}
	<div class="flex items-center gap-4">
		<Skeleton class="size-16 shrink-0 rounded-xl" />
		<div class="flex min-w-0 flex-col gap-2">
			<Skeleton class="h-7 w-56 max-w-full" />
			<Skeleton class="h-4 w-80 max-w-full" />
		</div>
	</div>
	<Skeleton class="h-48 rounded-xl" />
	<Skeleton class="h-64 rounded-xl" />
{:else}
	<PageHeader title={packageTitle(pkg)} description={pkg.summary}>
		{#snippet media()}
			<PackageIcon iconId={pkg.iconImageId} alt={packageTitle(pkg)} class="size-16 rounded-xl" />
		{/snippet}
		<div class="flex flex-wrap items-center gap-1.5 pt-1">
			<KindBadge kind={pkg.kind} />
			<span class="inline-flex max-w-full min-w-0 items-center">
				<Badge variant="outline" class="max-w-full min-w-0 shrink font-mono"><span class="truncate">{pkg.flatpakId}</span></Badge>
				<CopyButton value={pkg.flatpakId} label="Copy flatpak ID" size="icon-xs" />
			</span>
			{#if pkg.version}<Badge variant="secondary" class="max-w-full"><span class="truncate">v{pkg.version}</span></Badge>{/if}
			<ExtraDataBadge hasExtraData={pkg.hasExtraData} />
		</div>
		<div class="flex flex-wrap items-center gap-1.5 pt-1">
			{#each pkg.registries as registry (registry.id)}
				<Badge variant="secondary" href="/registries/{registry.id}" class="max-w-full">
					<ServerIcon data-icon="inline-start" /><span class="truncate">{registry.name}</span>
				</Badge>
			{/each}
		</div>
	</PageHeader>

	<Card.Root class="gap-0 py-0">
		<Card.Header class="border-b py-4">
			<Card.Title>Variants</Card.Title>
			<Card.Description>
				{variants.length.toLocaleString()}
				{variants.length === 1 ? 'image' : 'images'} ·
				{architectures.length}
				{architectures.length === 1 ? 'architecture' : 'architectures'} ·
				{branches.length}
				{branches.length === 1 ? 'branch' : 'branches'}. Select one to see its details.
			</Card.Description>
		</Card.Header>
		<VariantsTable {variants} {selectedId} onselect={select} />
	</Card.Root>

	<section bind:this={detailsSection} class="flex scroll-mt-20 flex-col gap-4" aria-labelledby="variant-heading">
		{#if staleVariant}
			<Alert.Root>
				<InfoIcon />
				<Alert.Title>The linked image is no longer indexed</Alert.Title>
				<Alert.Description>
					<span>
						Image <span class="font-mono text-xs break-all">{staleVariant.requested}</span> was removed from its
						registry{staleVariant.shown ? `; showing ${staleVariant.shown} instead` : ''}.
					</span>
				</Alert.Description>
			</Alert.Root>
		{/if}
		{#if selected}
			<div class="flex min-w-0 flex-col gap-1">
				<h2 id="variant-heading" class="text-lg font-semibold tracking-tight">Variant details</h2>
				<p class="font-mono text-xs [overflow-wrap:anywhere] text-muted-foreground">
					{selected.ref} · {selected.registryName} · {selected.repository}{selected.tags.length
						? `:${selected.tags[0]}`
						: ''}
				</p>
			</div>
			{#if detail.error && !detail.loading}
				<ErrorAlert error={detail.error} onretry={detail.refresh} />
			{:else if shown}
				<div class={cn('transition-opacity', detailStale && 'opacity-60')} aria-busy={detailStale}>
					<ImageDetails detail={shown} />
				</div>
			{:else}
				<div class="grid gap-4 lg:grid-cols-2">
					<Skeleton class="h-72 rounded-xl" />
					<Skeleton class="h-72 rounded-xl" />
				</div>
			{/if}
		{:else}
			<h2 id="variant-heading" class="sr-only">Variant details</h2>
			<p class="text-sm text-muted-foreground">No variants indexed.</p>
		{/if}
	</section>
{/if}
