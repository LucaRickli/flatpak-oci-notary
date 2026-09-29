<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Empty from '$lib/components/ui/empty';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import PageHeader from '$lib/components/page-header.svelte';
	import ExternalSyncNote from '$lib/components/external-sync-note.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import RegistriesTable from '$lib/components/registries-table.svelte';
	import RepositoriesTable from '$lib/components/repositories-table.svelte';
	import { imageClient, RefKind, registryClient, repositoryClient, systemClient, type Registry } from '$lib/api';
	import PackageIconView from '$lib/components/package-icon.svelte';
	import { packageHref } from '$lib/links';
	import { packageTitle } from '$lib/format';
	import { Resource } from '$lib/resource.svelte';
	import { onSyncEnded, poll } from '$lib/poll.svelte';
	import { isSyncing, POLL_LIST_MS, startSync, syncPollInterval } from '$lib/sync';
	import { cn } from '$lib/utils/shadcn';
	import ServerIcon from '@lucide/svelte/icons/server';
	import PackageIcon from '@lucide/svelte/icons/package';
	import LibraryIcon from '@lucide/svelte/icons/library';
	import LayoutGridIcon from '@lucide/svelte/icons/layout-grid';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';

	const showcase = new Resource(() => imageClient.listPackages({ kind: RefKind.APP, pageSize: 12 }));
	const overview = new Resource(() => systemClient.getOverview({}));
	const registries = new Resource(async () => (await registryClient.listRegistries({})).registries);
	const repositories = new Resource(async () => (await repositoryClient.listRepositories({})).repositories);

	const anySyncing = $derived(registries.current?.some(isSyncing) ?? false);

	// Poll the registry state (often while a sync runs or waits for a syncer, slowly otherwise, to
	// notice syncs started elsewhere). The counts grow while a sync runs: refresh them in the
	// background, and once more whenever a sync has ended.
	poll(() => syncPollInterval(registries.current), registries.poll);
	poll(() => (anySyncing ? POLL_LIST_MS : undefined), overview.poll);
	onSyncEnded(
		() => registries.current,
		() => {
			overview.poll();
			repositories.poll();
			showcase.poll();
		}
	);

	async function sync(registry: Registry) {
		const updated = await startSync(registry);
		if (updated && registries.current) {
			registries.current = registries.current.map((r) => (r.id === updated.id ? updated : r));
		}
	}

	const steps = $derived([
		{ done: (overview.current?.registries ?? 0) > 0, title: 'Add a registry', text: 'ghcr.io, Fedora or any OCI registry.', href: '/registries/new', cta: 'Add registry' },
		{ done: (overview.current?.images ?? 0) > 0, title: 'Index flatpaks', text: 'Sync it to discover flatpak images.', href: '/registries', cta: 'Open registries' },
		{ done: (overview.current?.repositories ?? 0) > 0, title: 'Publish a repository', text: 'Serve a flatpak remote.', href: '/repositories/new', cta: 'New repository' }
	]);
	const setupDone = $derived(steps.every((s) => s.done));

	const o = $derived(overview.current);
	const stats = $derived([
		{
			label: 'Packages',
			href: '/packages',
			icon: LayoutGridIcon,
			tone: 'bg-primary/10 text-primary',
			value: o ? (o.apps + o.runtimes).toLocaleString() : undefined,
			sub: o ? `${o.apps.toLocaleString()} apps · ${o.runtimes.toLocaleString()} runtimes` : ''
		},
		{
			label: 'Images',
			href: '/packages',
			icon: PackageIcon,
			tone: 'bg-chart-2/15 text-chart-2',
			value: o?.images.toLocaleString(),
			sub: 'All arches and tags'
		},
		{
			label: 'Registries',
			href: '/registries',
			icon: ServerIcon,
			tone: 'bg-warning/12 text-warning',
			value: o?.registries.toLocaleString(),
			sub: anySyncing ? 'Syncing now' : 'Upstream sources'
		},
		{
			label: 'Repositories',
			href: '/repositories',
			icon: LibraryIcon,
			tone: 'bg-success/12 text-success',
			value: o?.repositories.toLocaleString(),
			sub: 'Published remotes'
		}
	]);
</script>

<PageHeader title="Overview" description="Flatpak remotes backed by OCI registries.">
	{#snippet actions()}
		<Button variant="outline" href="/registries/new"><PlusIcon data-icon="inline-start" />Add registry</Button>
		<Button href="/repositories/new" disabled={registries.current?.length === 0}>
			<PlusIcon data-icon="inline-start" />New repository
		</Button>
	{/snippet}
</PageHeader>

{#if overview.error}
	<ErrorAlert error={overview.error} onretry={overview.refresh} />
{/if}

<Card.Root class="py-0">
	<div class="grid grid-cols-2 divide-border lg:grid-cols-4 lg:divide-x [&>*:nth-child(-n+2)]:max-lg:border-b [&>*:nth-child(odd)]:max-lg:border-r">
		{#each stats as stat (stat.label)}
			<a href={stat.href} class="group flex items-center gap-3 p-4 transition-colors hover:bg-muted/50 md:p-5">
				<div class={cn('hidden size-10 shrink-0 items-center justify-center rounded-xl sm:flex', stat.tone)}>
					<stat.icon class="size-5" />
				</div>
				<div class="flex min-w-0 flex-col">
					<span class="text-xs font-medium text-muted-foreground">{stat.label}</span>
					{#if stat.value === undefined}
						<Skeleton class="my-1 h-7 w-12" />
					{:else}
						<span class="text-2xl font-semibold tabular-nums">{stat.value}</span>
					{/if}
					<span class="truncate text-xs text-muted-foreground">{stat.sub}</span>
				</div>
			</a>
		{/each}
	</div>
</Card.Root>

{#if overview.current && !setupDone}
	<Card.Root class="gap-3">
		<Card.Header>
			<Card.Title>Get started</Card.Title>
		</Card.Header>
		<Card.Content>
			<ol class="grid gap-3 md:grid-cols-3">
				{#each steps as step, i (step.title)}
					<li
						class={cn(
							'flex items-center gap-3 rounded-lg border p-3',
							step.done && 'border-transparent bg-success/8'
						)}
					>
						<div
							class={cn(
								'flex size-8 shrink-0 items-center justify-center rounded-full text-sm font-semibold',
								step.done ? 'bg-success text-background' : 'bg-primary/10 text-primary'
							)}
						>
							{#if step.done}<CircleCheckIcon class="size-4" />{:else}{i + 1}{/if}
						</div>
						<div class="flex min-w-0 flex-1 flex-col">
							<span class="text-sm font-medium">{step.title}</span>
							<span class="truncate text-xs text-muted-foreground">{step.text}</span>
						</div>
						{#if !step.done}
							<Button size="icon-sm" variant="ghost" href={step.href} aria-label={step.cta}><ArrowRightIcon /></Button>
						{/if}
					</li>
				{/each}
			</ol>
		</Card.Content>
	</Card.Root>
{/if}

<div class="grid gap-6 xl:grid-cols-2">
	<section class="flex min-w-0 flex-col gap-3" aria-labelledby="registries-heading">
		<div class="flex items-center justify-between">
			<h2 id="registries-heading" class="text-base font-semibold">Registries</h2>
			<Button variant="ghost" size="sm" href="/registries">View all<ArrowRightIcon data-icon="inline-end" /></Button>
		</div>
		{#if registries.error}
			<ErrorAlert error={registries.error} onretry={registries.refresh} />
		{:else if !registries.current}
			<Skeleton class="h-40 rounded-xl" />
		{:else if registries.current.length === 0}
			<Empty.Root class="border border-dashed p-6 md:p-6">
				<Empty.Header>
					<Empty.Media variant="icon"><ServerIcon /></Empty.Media>
					<Empty.Title>No registries</Empty.Title>
				</Empty.Header>
				<Empty.Content>
					<Button size="sm" href="/registries/new"><PlusIcon data-icon="inline-start" />Add registry</Button>
				</Empty.Content>
			</Empty.Root>
		{:else}
			<Card.Root class="py-0">
				<RegistriesTable registries={registries.current} onsync={sync} compact />
			</Card.Root>
			<ExternalSyncNote />
		{/if}
	</section>

	<section class="flex min-w-0 flex-col gap-3" aria-labelledby="repositories-heading">
		<div class="flex items-center justify-between">
			<h2 id="repositories-heading" class="text-base font-semibold">Repositories</h2>
			<Button variant="ghost" size="sm" href="/repositories">View all<ArrowRightIcon data-icon="inline-end" /></Button>
		</div>
		{#if repositories.error}
			<ErrorAlert error={repositories.error} onretry={repositories.refresh} />
		{:else if !repositories.current}
			<Skeleton class="h-40 rounded-xl" />
		{:else if repositories.current.length === 0}
			<Empty.Root class="border border-dashed p-6 md:p-6">
				<Empty.Header>
					<Empty.Media variant="icon"><LibraryIcon /></Empty.Media>
					<Empty.Title>No repositories</Empty.Title>
				</Empty.Header>
				<Empty.Content>
					<Button size="sm" href="/repositories/new" disabled={registries.current?.length === 0}>
						<PlusIcon data-icon="inline-start" />New repository
					</Button>
				</Empty.Content>
			</Empty.Root>
		{:else}
			<Card.Root class="py-0">
				<RepositoriesTable repositories={repositories.current} compact />
			</Card.Root>
		{/if}
	</section>
</div>

{#if showcase.current && showcase.current.packages.length > 0}
	<section class="flex flex-col gap-3" aria-labelledby="catalog-heading">
		<div class="flex items-center justify-between">
			<h2 id="catalog-heading" class="text-base font-semibold">Apps in the catalog</h2>
			<Button variant="ghost" size="sm" href="/packages?kind=app">Browse<ArrowRightIcon data-icon="inline-end" /></Button>
		</div>
		<Card.Root>
			<Card.Content>
				<ul class="grid grid-cols-4 gap-x-1 gap-y-3 md:grid-cols-6 max-md:[&>li:nth-child(n+9)]:hidden">
					{#each showcase.current.packages as pkg (pkg.flatpakId)}
						<li>
							<a
								href={packageHref(pkg.kind, pkg.flatpakId)}
								class="group flex flex-col items-center gap-2 rounded-lg p-2 text-center transition-colors hover:bg-muted/60"
							>
								<PackageIconView
									iconId={pkg.iconImageId}
									alt=""
									class="size-14 rounded-xl bg-transparent transition-transform group-hover:scale-105"
								/>
								<span class="line-clamp-1 w-full text-xs font-medium">{packageTitle(pkg)}</span>
							</a>
						</li>
					{/each}
				</ul>
			</Card.Content>
		</Card.Root>
	</section>
{/if}
