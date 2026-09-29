<script lang="ts">
	import { afterNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import { tick } from 'svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import * as Select from '$lib/components/ui/select';
	import * as Empty from '$lib/components/ui/empty';
	import * as Alert from '$lib/components/ui/alert';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import InstallPanel from '$lib/components/install-panel.svelte';
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
	import { formatBytes, formatDate, formatRelative, packageTitle, toDate } from '$lib/format';
	import { refHref, VARIANT_PARAM } from '$lib/links';
	import { canonicalId } from '$lib/ids';
	import { cn } from '$lib/utils/shadcn';
	import ServerIcon from '@lucide/svelte/icons/server';
	import InfoIcon from '@lucide/svelte/icons/info';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import PackageXIcon from '@lucide/svelte/icons/package-x';

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

	async function select(variant: Image, scroll = true) {
		requested = variant.id;
		staleVariant = undefined;
		await setVariantParam(variant.id);
		await tick();
		// On small screens the details are far below the table: bring them into view.
		if (scroll && detailsSection && detailsSection.getBoundingClientRect().top > window.innerHeight) {
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

	// Variant picker: branch and architecture segments, plus the source when several images share
	// both (other registries, repositories or tags).
	const archOf = (v: Image) => v.arch || v.architecture;
	const branchOptions = $derived([...new Set(variants.map((v) => v.branch))].sort());
	const archOptions = $derived([...new Set(variants.map(archOf))].sort());
	const branchArches = $derived(new Set(variants.filter((v) => v.branch === selected?.branch).map(archOf)));
	const sources = $derived(
		selected ? variants.filter((v) => v.branch === selected.branch && archOf(v) === archOf(selected)) : []
	);
	const sourceLabel = (v: Image) => `${v.registryName} · ${v.repository}${v.tags.length ? `:${v.tags[0]}` : ''}`;

	function pick(match: (v: Image) => boolean, prefer: (v: Image) => boolean) {
		const pool = variants.filter(match);
		const best = defaultVariant(pool.filter(prefer).length ? pool.filter(prefer) : pool);
		if (best && best.id !== selectedId) select(best, false);
	}
	const pickBranch = (b: string) =>
		b && pick((v) => v.branch === b, (v) => !!selected && archOf(v) === archOf(selected));
	const pickArch = (a: string) =>
		a && pick((v) => archOf(v) === a, (v) => !!selected && v.branch === selected.branch);

	let allOpen = $state(false);
	const versionLabel = $derived(pkg?.version && /^\d/.test(pkg.version) ? `v${pkg.version}` : (pkg?.version ?? ''));
	const runtimeLink = $derived(selected?.runtime ? refHref(selected.runtime) : undefined);
</script>

{#snippet stat(label: string, value: string, title?: string)}
	<div class="flex min-w-0 flex-col gap-0.5">
		<dt class="text-xs text-muted-foreground">{label}</dt>
		<dd class="truncate text-sm font-medium tabular-nums" {title}>{value || '—'}</dd>
	</div>
{/snippet}

{#if resource.error}
	{#if isNotFound(resource.error)}
		<Empty.Root>
			<Empty.Header>
				<Empty.Media variant="icon"><PackageXIcon /></Empty.Media>
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
	<div class="flex items-center gap-5">
		<Skeleton class="size-20 shrink-0 rounded-3xl md:size-28" />
		<div class="flex min-w-0 flex-col gap-2">
			<Skeleton class="h-9 w-56 max-w-full" />
			<Skeleton class="h-4 w-80 max-w-full" />
		</div>
	</div>
	<Skeleton class="h-32 rounded-xl" />
	<Skeleton class="h-64 rounded-xl" />
{:else}
	<section class="flex flex-col gap-6 lg:flex-row lg:items-start">
		<div class="flex min-w-0 flex-1 items-start gap-4 md:gap-6">
			<PackageIcon
				iconId={pkg.iconImageId}
				alt={packageTitle(pkg)}
				class="size-20 rounded-3xl bg-card shadow-sm ring-1 ring-foreground/10 md:size-28"
			/>
			<div class="flex min-w-0 flex-col gap-1.5">
				<h1 class="text-2xl font-bold [overflow-wrap:anywhere] md:text-4xl">{packageTitle(pkg)}</h1>
				<div class="flex min-w-0 items-center gap-0.5 text-muted-foreground">
					<span class="truncate font-mono text-sm">{pkg.flatpakId}</span>
					<CopyButton value={pkg.flatpakId} label="Copy flatpak ID" size="icon-xs" />
				</div>
				{#if pkg.summary}<p class="text-base text-pretty">{pkg.summary}</p>{/if}
				<div class="flex flex-wrap items-center gap-1.5 pt-1">
					<KindBadge kind={pkg.kind} />
					{#if pkg.version}<Badge variant="secondary" class="max-w-full"><span class="truncate">{versionLabel}</span></Badge>{/if}
					<ExtraDataBadge hasExtraData={pkg.hasExtraData} />
					{#each pkg.registries as registry (registry.id)}
						<Badge variant="outline" href="/registries/{registry.id}" class="max-w-full">
							<ServerIcon data-icon="inline-start" /><span class="truncate">{registry.name}</span>
						</Badge>
					{/each}
				</div>
			</div>
		</div>
		<InstallPanel {pkg} variant={selected} class="w-full shrink-0 lg:w-96" />
	</section>

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
		<Collapsible.Root bind:open={allOpen}>
			<Card.Root class="gap-0 py-0">
				<div class="flex flex-wrap items-center gap-x-5 gap-y-3 px-4 py-3">
					<div class="flex items-center gap-2">
						<span class="text-xs font-medium text-muted-foreground" id="branch-label">Branch</span>
						{#if branchOptions.length > 1}
							<ToggleGroup.Root
								type="single"
								variant="segmented"
								spacing={0.5}
								size="sm"
								value={selected.branch}
								onValueChange={pickBranch}
								aria-labelledby="branch-label"
							>
								{#each branchOptions as b (b)}
									<ToggleGroup.Item value={b} class="font-mono">{b || '—'}</ToggleGroup.Item>
								{/each}
							</ToggleGroup.Root>
						{:else}
							<Badge variant="muted" class="font-mono">{selected.branch || '—'}</Badge>
						{/if}
					</div>
					<div class="flex items-center gap-2">
						<span class="text-xs font-medium text-muted-foreground" id="arch-label">Arch</span>
						{#if archOptions.length > 1}
							<ToggleGroup.Root
								type="single"
								variant="segmented"
								spacing={0.5}
								size="sm"
								value={archOf(selected)}
								onValueChange={pickArch}
								aria-labelledby="arch-label"
							>
								{#each archOptions as a (a)}
									<ToggleGroup.Item value={a} class="font-mono" disabled={!branchArches.has(a)}>{a}</ToggleGroup.Item>
								{/each}
							</ToggleGroup.Root>
						{:else}
							<Badge variant="muted" class="font-mono">{archOf(selected)}</Badge>
						{/if}
					</div>
					{#if sources.length > 1}
						<Select.Root
							type="single"
							value={selected.id}
							onValueChange={(id) => {
								const v = variants.find((x) => x.id === id);
								if (v) select(v, false);
							}}
						>
							<Select.Trigger size="sm" class="max-w-full min-w-0 sm:max-w-80" aria-label="Source">
								<ServerIcon /><span class="truncate">{sourceLabel(selected)}</span>
							</Select.Trigger>
							<Select.Content>
								<Select.Group>
									{#each sources as v (v.id)}
										<Select.Item value={v.id}>{sourceLabel(v)}</Select.Item>
									{/each}
								</Select.Group>
							</Select.Content>
						</Select.Root>
					{/if}
					<Collapsible.Trigger>
						{#snippet child({ props })}
							<Button {...props} variant="ghost" size="sm" class="ml-auto">
								All {variants.length}
								{variants.length === 1 ? 'variant' : 'variants'}
								<ChevronDownIcon data-icon="inline-end" class={cn('transition-transform', allOpen && 'rotate-180')} />
							</Button>
						{/snippet}
					</Collapsible.Trigger>
				</div>
				<Collapsible.Content>
					<div class="border-t">
						<VariantsTable {variants} {selectedId} onselect={select} />
					</div>
				</Collapsible.Content>
				<dl
					class="grid grid-cols-2 gap-x-6 gap-y-3 border-t bg-muted/30 px-4 py-3 sm:grid-cols-3 lg:grid-cols-6"
				>
					{@render stat('Version', selected.version)}
					{@render stat('Download', formatBytes(selected.downloadSize))}
					{@render stat('Installed', formatBytes(selected.installedSize))}
					{@render stat('Built', selected.created ? formatRelative(selected.created) : '', formatDate(selected.created))}
					<div class="col-span-2 flex min-w-0 flex-col gap-0.5">
						<dt class="text-xs text-muted-foreground">{selected.runtime ? 'Runtime' : 'Source'}</dt>
						<dd class="truncate font-mono text-xs leading-5">
							{#if selected.runtime && runtimeLink}
								<a href={runtimeLink} class="text-primary hover:underline" title={selected.runtime}>{selected.runtime}</a>
							{:else}
								<span title={sourceLabel(selected)}>{selected.repository}</span>
							{/if}
						</dd>
					</div>
				</dl>
			</Card.Root>
		</Collapsible.Root>
	{/if}

	<section bind:this={detailsSection} class="flex scroll-mt-20 flex-col gap-4" aria-labelledby="variant-heading">
		<h2 id="variant-heading" class="sr-only">Variant details</h2>
		{#if selected}
			{#if detail.error && !detail.loading}
				<ErrorAlert error={detail.error} onretry={detail.refresh} />
			{:else if shown}
				<div class={cn('transition-opacity', detailStale && 'opacity-60')} aria-busy={detailStale}>
					<ImageDetails detail={shown} />
				</div>
			{:else}
				<Skeleton class="h-8 w-80 max-w-full" />
				<div class="grid gap-4 lg:grid-cols-2">
					<Skeleton class="h-72 rounded-xl" />
					<Skeleton class="h-72 rounded-xl" />
				</div>
			{/if}
		{:else}
			<p class="text-sm text-muted-foreground">No variants indexed.</p>
		{/if}
	</section>
{/if}
