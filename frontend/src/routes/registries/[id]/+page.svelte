<script lang="ts">
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import * as Card from '$lib/components/ui/card';
	import * as Tabs from '$lib/components/ui/tabs';
	import * as Alert from '$lib/components/ui/alert';
	import * as Empty from '$lib/components/ui/empty';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { Spinner } from '$lib/components/ui/spinner';
	import PageHeader from '$lib/components/page-header.svelte';
	import SyncStateBadge from '$lib/components/sync-state-badge.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import StatCard from '$lib/components/stat-card.svelte';
	import ImagesTable from '$lib/components/images-table.svelte';
	import TableSkeleton from '$lib/components/table-skeleton.svelte';
	import RegistryForm from '$lib/components/registry-form.svelte';
	import ConfirmDelete from '$lib/components/confirm-delete.svelte';
	import { imageClient, registryClient, SyncState } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { isNotFound, reportError } from '$lib/session.svelte';
	import { setCrumb } from '$lib/breadcrumb.svelte';
	import { startSync } from '$lib/sync';
	import { formatDate, formatDuration, formatInterval, formatRelative } from '$lib/format';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import PackageSearchIcon from '@lucide/svelte/icons/package-search';
	import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';

	let { data } = $props();

	const resource = new Resource(async () => (await registryClient.getRegistry({ id: data.id })).registry);
	const images = new Resource(async () => (await imageClient.listImages({ registryId: data.id })).images);

	// Ignore stale data from the previously viewed registry while the next one loads.
	const registry = $derived(resource.current?.id === data.id ? resource.current : undefined);
	const imageList = $derived(images.current?.every((i) => i.registryId === data.id) ? images.current : undefined);
	const syncing = $derived(registry?.syncState === SyncState.SYNCING);

	let tab = $state('packages');
	let syncRequested = $state(false);

	$effect(() => {
		if (registry) setCrumb(registry.name);
	});

	// Poll while syncing; when a sync finishes, reload the images and report the outcome.
	let pollingId: number | undefined;
	$effect(() => {
		const r = registry;
		if (!r) return;
		if (r.syncState !== SyncState.SYNCING) {
			if (pollingId === r.id) {
				pollingId = undefined;
				images.refresh();
				if (r.syncState === SyncState.ERROR) toast.error(`Sync of ${r.name} failed`, { description: r.lastSyncError });
				else toast.success(`Sync of ${r.name} finished`, { description: `${r.imageCount} flatpak images indexed` });
			}
			return;
		}
		pollingId = r.id;
		const timer = setTimeout(resource.refresh, 2000);
		return () => clearTimeout(timer);
	});

	async function sync() {
		if (!registry) return;
		syncRequested = true;
		const updated = await startSync(registry);
		if (updated) resource.current = updated;
		syncRequested = false;
	}

	async function remove() {
		if (!registry) return;
		try {
			await registryClient.deleteRegistry({ id: registry.id });
			toast.success(`Registry ${registry.name} deleted`);
			goto('/registries');
		} catch (err) {
			reportError(err, 'Could not delete registry');
			return false;
		}
	}
</script>

{#if resource.error}
	{#if isNotFound(resource.error)}
		<Empty.Root>
			<Empty.Header>
				<Empty.Title>Registry not found</Empty.Title>
				<Empty.Description>It may have been deleted.</Empty.Description>
			</Empty.Header>
			<Empty.Content><Button href="/registries" variant="outline">Back to registries</Button></Empty.Content>
		</Empty.Root>
	{:else}
		<ErrorAlert error={resource.error} onretry={resource.refresh} />
	{/if}
{:else if !registry}
	<div class="flex flex-col gap-2">
		<Skeleton class="h-8 w-64" />
		<Skeleton class="h-4 w-48" />
	</div>
	<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
		{#each { length: 4 }, i (i)}<Skeleton class="h-24 rounded-xl" />{/each}
	</div>
{:else}
	<PageHeader title={registry.name}>
		<span class="font-mono text-sm break-all text-muted-foreground">{registry.url}</span>
		{#snippet actions()}
			<Button onclick={sync} disabled={syncing || syncRequested}>
				{#if syncing || syncRequested}<Spinner data-icon="inline-start" />{:else}<RefreshCwIcon data-icon="inline-start" />{/if}
				{syncing ? 'Syncing…' : 'Sync now'}
			</Button>
			<ConfirmDelete
				title="Delete {registry.name}?"
				description="The registry and its indexed images are removed from the notary. Images on the registry itself are not touched."
				onconfirm={remove}
			/>
		{/snippet}
	</PageHeader>

	{#if registry.syncState === SyncState.ERROR && registry.lastSyncError}
		<Alert.Root variant="destructive">
			<CircleAlertIcon />
			<Alert.Title>Last sync failed</Alert.Title>
			<Alert.Description class="font-mono text-xs break-all">{registry.lastSyncError}</Alert.Description>
		</Alert.Root>
	{/if}

	<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
		<Card.Root size="sm">
			<Card.Header>
				<Card.Description>Status</Card.Description>
				<Card.Title><SyncStateBadge {registry} /></Card.Title>
			</Card.Header>
			<Card.Content class="text-xs text-muted-foreground">
				{#if registry.lastSyncAt}
					<span title={formatDate(registry.lastSyncAt)}>Last sync {formatRelative(registry.lastSyncAt)}</span>
					· took {formatDuration(registry.lastSyncDurationMs)}
				{:else}
					Not synced yet
				{/if}
			</Card.Content>
		</Card.Root>
		<StatCard label="Flatpak images" value={registry.imageCount}>All architectures and tags</StatCard>
		<StatCard label="OCI repositories" value={registry.repositoryCount}>Containing flatpak images</StatCard>
		<StatCard label="Schedule" value={formatInterval(registry.syncIntervalMinutes)}>
			{registry.useCatalog ? 'Catalog discovery' : 'No catalog'} · {registry.repositories.length} explicit
			{registry.repositories.length === 1 ? 'repository' : 'repositories'}
		</StatCard>
	</div>

	<Tabs.Root bind:value={tab}>
		<Tabs.List>
			<Tabs.Trigger value="packages">Packages</Tabs.Trigger>
			<Tabs.Trigger value="settings">Settings</Tabs.Trigger>
		</Tabs.List>
		<Tabs.Content value="packages" class="pt-2">
			{#if images.error}
				<ErrorAlert error={images.error} onretry={images.refresh} />
			{:else if !imageList}
				<Card.Root><Card.Content><TableSkeleton /></Card.Content></Card.Root>
			{:else if imageList.length === 0}
				<Empty.Root class="border border-dashed">
					<Empty.Header>
						<Empty.Media variant="icon"><PackageSearchIcon /></Empty.Media>
						{#if registry.syncState === SyncState.NEVER || !registry.lastSyncAt}
							<Empty.Title>Not synced yet</Empty.Title>
							<Empty.Description>Sync the registry to index its flatpak images.</Empty.Description>
						{:else}
							<Empty.Title>No flatpak images found</Empty.Title>
							<Empty.Description>
								No indexed image carries the org.flatpak.ref label. Check the discovery settings: without
								catalog support, repositories must be listed explicitly.
							</Empty.Description>
						{/if}
					</Empty.Header>
					<Empty.Content class="flex-row justify-center">
						<Button onclick={sync} disabled={syncing || syncRequested}>
							{#if syncing}<Spinner data-icon="inline-start" />{:else}<RefreshCwIcon data-icon="inline-start" />{/if}
							{syncing ? 'Syncing…' : 'Sync now'}
						</Button>
						<Button variant="outline" onclick={() => (tab = 'settings')}>Edit discovery settings</Button>
					</Empty.Content>
				</Empty.Root>
			{:else}
				<Card.Root class="py-0">
					<ImagesTable images={imageList} showRegistry={false} />
				</Card.Root>
				<div class="mt-3 flex justify-end">
					<Button variant="link" href="/packages?registry={registry.id}">
						Browse in Packages<ArrowRightIcon data-icon="inline-end" />
					</Button>
				</div>
			{/if}
		</Tabs.Content>
		<Tabs.Content value="settings" class="pt-2">
			{#key registry.id}
				<RegistryForm
					{registry}
					onsaved={(r) => {
						resource.current = r;
						images.refresh();
					}}
				/>
			{/key}
		</Tabs.Content>
	</Tabs.Root>
{/if}
