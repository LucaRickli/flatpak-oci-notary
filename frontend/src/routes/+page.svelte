<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Item from '$lib/components/ui/item';
	import * as Empty from '$lib/components/ui/empty';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import PageHeader from '$lib/components/page-header.svelte';
	import StatCard from '$lib/components/stat-card.svelte';
	import SyncStateBadge from '$lib/components/sync-state-badge.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import CopyButton from '$lib/components/copy-button.svelte';
	import { registryClient, repositoryClient, SyncState, systemClient, type Registry } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { startSync } from '$lib/sync';
	import { formatRelative } from '$lib/format';
	import { cn } from '$lib/utils/shadcn';
	import ServerIcon from '@lucide/svelte/icons/server';
	import PackageIcon from '@lucide/svelte/icons/package';
	import LibraryIcon from '@lucide/svelte/icons/library';
	import AppWindowIcon from '@lucide/svelte/icons/app-window';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import CircleIcon from '@lucide/svelte/icons/circle';

	const overview = new Resource(() => systemClient.getOverview({}));
	const registries = new Resource(async () => (await registryClient.listRegistries({})).registries);
	const repositories = new Resource(async () => (await repositoryClient.listRepositories({})).repositories);

	const anySyncing = $derived(registries.current?.some((r) => r.syncState === SyncState.SYNCING) ?? false);

	// Poll while syncing; refresh the counts once all syncs are done.
	let wasSyncing = false;
	$effect(() => {
		if (!anySyncing) {
			if (wasSyncing) overview.refresh();
			wasSyncing = false;
			return;
		}
		wasSyncing = true;
		const timer = setTimeout(registries.refresh, 2000);
		return () => clearTimeout(timer);
	});

	async function sync(registry: Registry) {
		const updated = await startSync(registry);
		if (updated && registries.current) {
			registries.current = registries.current.map((r) => (r.id === updated.id ? updated : r));
		}
	}

	const steps = $derived([
		{ done: (overview.current?.registries ?? 0) > 0, title: 'Add a registry', text: 'Connect ghcr.io, a Fedora or any other OCI registry.', href: '/registries/new', cta: 'Add registry' },
		{ done: (overview.current?.images ?? 0) > 0, title: 'Index flatpaks', text: 'Sync the registry to discover images labelled with org.flatpak.ref.', href: '/registries', cta: 'Open registries' },
		{ done: (overview.current?.repositories ?? 0) > 0, title: 'Publish a repository', text: 'Pick packages and share the remote-add command.', href: '/repositories/new', cta: 'New repository' }
	]);
	const setupDone = $derived(steps.every((s) => s.done));
</script>

<PageHeader title="Dashboard" description="Flatpak remotes backed by OCI registries." />

{#if overview.error}
	<ErrorAlert error={overview.error} onretry={overview.refresh} />
{/if}

<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
	<StatCard label="Registries" icon={ServerIcon} href="/registries" value={overview.current?.registries} />
	<StatCard label="Packages" icon={PackageIcon} href="/packages" value={overview.current?.images}>
		Flatpak images, all arches and tags
	</StatCard>
	<StatCard label="Apps / runtimes" icon={AppWindowIcon} href="/packages?kind=app" value={overview.current ? `${overview.current.apps} / ${overview.current.runtimes}` : undefined} />
	<StatCard label="Repositories" icon={LibraryIcon} href="/repositories" value={overview.current?.repositories} />
</div>

{#if overview.current && !setupDone}
	<Card.Root>
		<Card.Header>
			<Card.Title>Get started</Card.Title>
			<Card.Description>Three steps to serve flatpaks from an OCI registry.</Card.Description>
		</Card.Header>
		<Card.Content class="grid gap-3 md:grid-cols-3">
			{#each steps as step, i (step.title)}
				<Item.Root variant="outline" class="items-start">
					<Item.Media variant="icon" class={cn(step.done ? 'text-primary' : 'text-muted-foreground')}>
						{#if step.done}<CircleCheckIcon />{:else}<CircleIcon />{/if}
					</Item.Media>
					<Item.Content>
						<Item.Title>{i + 1}. {step.title}</Item.Title>
						<Item.Description>{step.text}</Item.Description>
						{#if !step.done}
							<div class="pt-2"><Button size="sm" variant="outline" href={step.href}>{step.cta}</Button></div>
						{/if}
					</Item.Content>
				</Item.Root>
			{/each}
		</Card.Content>
	</Card.Root>
{/if}

<div class="grid gap-4 lg:grid-cols-2">
	<Card.Root>
		<Card.Header>
			<Card.Title>Registry status</Card.Title>
			<Card.Description>Indexing state of the upstream registries.</Card.Description>
			<Card.Action><Button variant="ghost" size="sm" href="/registries">View all</Button></Card.Action>
		</Card.Header>
		<Card.Content class="flex flex-col gap-2">
			{#if registries.error}
				<ErrorAlert error={registries.error} onretry={registries.refresh} />
			{:else if !registries.current}
				{#each { length: 3 }, i (i)}<Skeleton class="h-14 rounded-lg" />{/each}
			{:else if registries.current.length === 0}
				<Empty.Root class="p-6 md:p-6">
					<Empty.Header>
						<Empty.Title>No registries</Empty.Title>
						<Empty.Description>Add a registry to start indexing flatpaks.</Empty.Description>
					</Empty.Header>
					<Empty.Content>
						<Button size="sm" href="/registries/new"><PlusIcon data-icon="inline-start" />Add registry</Button>
					</Empty.Content>
				</Empty.Root>
			{:else}
				{#each registries.current as registry (registry.id)}
					{@const syncing = registry.syncState === SyncState.SYNCING}
					<Item.Root variant="outline" size="sm">
						<Item.Content class="min-w-0">
							<Item.Title class="w-full">
								<a href="/registries/{registry.id}" class="truncate hover:underline">{registry.name}</a>
							</Item.Title>
							<Item.Description class="truncate">
								{registry.imageCount} images · synced {formatRelative(registry.lastSyncAt)}
							</Item.Description>
						</Item.Content>
						<Item.Actions>
							<SyncStateBadge {registry} />
							<Button variant="ghost" size="icon-sm" aria-label="Sync {registry.name} now" title="Sync now" disabled={syncing} onclick={() => sync(registry)}>
								<RefreshCwIcon class={cn(syncing && 'animate-spin')} />
							</Button>
						</Item.Actions>
					</Item.Root>
				{/each}
			{/if}
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title>Repositories</Card.Title>
			<Card.Description>Published flatpak remotes.</Card.Description>
			<Card.Action><Button variant="ghost" size="sm" href="/repositories">View all</Button></Card.Action>
		</Card.Header>
		<Card.Content class="flex flex-col gap-2">
			{#if repositories.error}
				<ErrorAlert error={repositories.error} onretry={repositories.refresh} />
			{:else if !repositories.current}
				{#each { length: 3 }, i (i)}<Skeleton class="h-14 rounded-lg" />{/each}
			{:else if repositories.current.length === 0}
				<Empty.Root class="p-6 md:p-6">
					<Empty.Header>
						<Empty.Title>No repositories</Empty.Title>
						<Empty.Description>Publish a remote with a selection of indexed packages.</Empty.Description>
					</Empty.Header>
					<Empty.Content>
						<Button size="sm" href="/repositories/new" disabled={registries.current?.length === 0}>
							<PlusIcon data-icon="inline-start" />New repository
						</Button>
					</Empty.Content>
				</Empty.Root>
			{:else}
				{#each repositories.current as repo (repo.id)}
					<Item.Root variant="outline" size="sm">
						<Item.Content class="min-w-0">
							<Item.Title class="w-full">
								<a href="/repositories/{repo.id}" class="truncate hover:underline">{repo.title || repo.slug}</a>
							</Item.Title>
							<Item.Description class="truncate font-mono text-xs">{repo.urls?.remote}</Item.Description>
						</Item.Content>
						<Item.Actions>
							<span class="text-xs text-muted-foreground">{repo.imageCount} images</span>
							{#if repo.urls?.remote}
								<CopyButton value={`flatpak remote-add --if-not-exists --no-gpg-verify ${repo.slug} ${repo.urls.remote}`} label="Copy remote-add command" />
							{/if}
						</Item.Actions>
					</Item.Root>
				{/each}
			{/if}
		</Card.Content>
	</Card.Root>
</div>
