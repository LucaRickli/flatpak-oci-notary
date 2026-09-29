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
	import SyncProgress from '$lib/components/sync-progress.svelte';
	import SyncUpdatingNote from '$lib/components/sync-updating-note.svelte';
	import ExternalSyncNote from '$lib/components/external-sync-note.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import PaginatedTable from '$lib/components/paginated-table.svelte';
	import PackageCard from '$lib/components/package-card.svelte';
	import RegistryForm from '$lib/components/registry-form.svelte';
	import ConfirmDelete from '$lib/components/confirm-delete.svelte';
	import { imageClient, registryClient, SyncState } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { onSyncEnded, poll } from '$lib/poll.svelte';
	import { isNotFound, reportError } from '$lib/session.svelte';
	import { setCrumb } from '$lib/breadcrumb.svelte';
	import { isQueued, isQueuedBehind, isSyncing, POLL_LIST_MS, startSync, syncAction, syncPollInterval } from '$lib/sync';
	import { PAGE_SIZES, pageRequest } from '$lib/pagination';
	import { formatDate, formatDuration, formatInterval, formatRelative } from '$lib/format';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import HourglassIcon from '@lucide/svelte/icons/hourglass';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import PackageSearchIcon from '@lucide/svelte/icons/package-search';
	import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';
	import ServerIcon from '@lucide/svelte/icons/server';
	import LayoutGridIcon from '@lucide/svelte/icons/layout-grid';
	import SettingsIcon from '@lucide/svelte/icons/settings';

	let { data } = $props();

	// Packages tab pagination. The page is overridable and goes back to 1 when another registry is shown.
	let page = $derived.by(() => {
		void data.id;
		return 1;
	});
	let pageSize = $state(PAGE_SIZES[0]);

	const resource = new Resource(async () => (await registryClient.getRegistry({ id: data.id })).registry);
	const packages = new Resource(async () => {
		const registryId = data.id;
		const res = await imageClient.listPackages({ registryId, ...pageRequest(page, pageSize) });
		return { ...res, registryId };
	});

	// Ignore stale data from the previously viewed registry while the next one loads.
	const registry = $derived(resource.current?.id === data.id ? resource.current : undefined);
	const packageList = $derived(packages.current?.registryId === data.id ? packages.current : undefined);
	const syncing = $derived(!!registry && isSyncing(registry));
	const queued = $derived(!!registry && isQueued(registry));
	const action = $derived(syncAction(registry));

	let tab = $state('packages');
	let syncRequested = $state(false);
	const syncDisabled = $derived(syncing || queued || syncRequested);

	$effect(() => {
		if (registry) setCrumb(registry.name);
	});

	// Poll the registry: often while a sync runs or waits for a syncer, slowly otherwise, to notice
	// syncs started elsewhere (scheduler, external syncer, another admin). While a sync runs, newly
	// indexed repositories show up, so refresh the packages in the background too.
	poll(() => syncPollInterval(registry ? [registry] : undefined), resource.poll);
	poll(() => (syncing ? POLL_LIST_MS : undefined), packages.poll);

	// When a sync ends (also one that started and finished between two polls), reload the
	// packages and report the outcome.
	onSyncEnded(
		() => (registry ? [registry] : undefined),
		(r) => {
			packages.poll();
			if (r.syncState === SyncState.ERROR) toast.error(`Sync of ${r.name} failed`, { description: r.lastSyncError });
			else toast.success(`Sync of ${r.name} finished`, { description: `${r.imageCount} flatpak images indexed` });
		}
	);

	async function sync() {
		if (!registry) return;
		syncRequested = true;
		const updated = await startSync(registry);
		if (updated && updated.id === data.id) resource.current = updated;
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

{#snippet syncButton()}
	<Button onclick={sync} disabled={syncDisabled} title={action.hint}>
		{#if syncing || syncRequested}
			<Spinner data-icon="inline-start" />
		{:else if queued}
			<HourglassIcon data-icon="inline-start" />
		{:else}
			<RefreshCwIcon data-icon="inline-start" />
		{/if}
		{action.label}
	</Button>
{/snippet}

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
		<Skeleton class="h-8 w-64 max-w-full" />
		<Skeleton class="h-4 w-48 max-w-full" />
	</div>
	<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
		{#each { length: 4 }, i (i)}<Skeleton class="h-24 rounded-xl" />{/each}
	</div>
{:else}
	<PageHeader title={registry.name}>
		{#snippet media()}
			<div class="flex size-12 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary md:size-14">
				<ServerIcon class="size-6" />
			</div>
		{/snippet}
		<span class="font-mono text-sm break-all text-muted-foreground">{registry.url}</span>
		{#snippet actions()}
			{@render syncButton()}
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

	<Card.Root class="py-0">
		<dl class="grid grid-cols-2 lg:grid-cols-4 lg:divide-x [&>*:nth-child(-n+2)]:max-lg:border-b [&>*:nth-child(odd)]:max-lg:border-r">
			<div class="flex min-w-0 flex-col gap-1.5 p-4">
				<dt class="text-xs font-medium text-muted-foreground">Status</dt>
				<dd class="flex flex-col gap-1.5">
					<SyncStateBadge {registry} />
					<SyncProgress {registry} />
					{#if queued || isQueuedBehind(registry)}
						<span class="text-xs text-muted-foreground">{action.hint}</span>
					{:else if registry.lastSyncAt && !syncing}
						<span class="text-xs text-muted-foreground" title={formatDate(registry.lastSyncAt)}>
							{formatRelative(registry.lastSyncAt)} · {formatDuration(registry.lastSyncDurationMs)}
						</span>
					{/if}
				</dd>
			</div>
			<div class="flex min-w-0 flex-col gap-0.5 p-4">
				<dt class="text-xs font-medium text-muted-foreground">Flatpak images</dt>
				<dd class="text-2xl font-semibold tabular-nums">{registry.imageCount.toLocaleString()}</dd>
			</div>
			<div class="flex min-w-0 flex-col gap-0.5 p-4">
				<dt class="text-xs font-medium text-muted-foreground">OCI repositories</dt>
				<dd class="text-2xl font-semibold tabular-nums">{registry.repositoryCount.toLocaleString()}</dd>
			</div>
			<div class="flex min-w-0 flex-col gap-0.5 p-4">
				<dt class="text-xs font-medium text-muted-foreground">Schedule</dt>
				<dd class="flex flex-col">
					<span class="text-base font-semibold">{formatInterval(registry.syncIntervalMinutes)}</span>
					<span class="truncate text-xs text-muted-foreground">
						{registry.useCatalog ? 'Catalog discovery' : 'No catalog'} · {registry.repositories.length} explicit
					</span>
				</dd>
			</div>
		</dl>
	</Card.Root>
	<ExternalSyncNote />

	<Tabs.Root bind:value={tab}>
		<Tabs.List variant="line" class="w-full justify-start border-b">
			<Tabs.Trigger value="packages" class="flex-none"><LayoutGridIcon />Packages</Tabs.Trigger>
			<Tabs.Trigger value="settings" class="flex-none"><SettingsIcon />Settings</Tabs.Trigger>
		</Tabs.List>
		<Tabs.Content value="packages" class="flex flex-col gap-3 pt-2">
			{#if syncing}<SyncUpdatingNote />{/if}
			<PaginatedTable
				result={packageList}
				loading={packages.loading}
				error={packages.error}
				onretry={packages.refresh}
				bind:page
				bind:pageSize
				framed={false}
			>
				{#snippet children(res)}
					<div class="grid grid-cols-[repeat(auto-fill,minmax(min(100%,17rem),1fr))] gap-3 md:gap-4">
						{#each res.packages as pkg (`${pkg.kind}/${pkg.flatpakId}`)}
							<PackageCard {pkg} showRegistry={false} />
						{/each}
					</div>
				{/snippet}
				{#snippet empty()}
					<Empty.Root class="border border-dashed">
						<Empty.Header>
							<Empty.Media variant="icon">
								{#if syncing}<Spinner />{:else}<PackageSearchIcon />{/if}
							</Empty.Media>
							{#if syncing}
								<Empty.Title>Indexing…</Empty.Title>
								<Empty.Description>Packages show up here as the sync indexes their repositories.</Empty.Description>
							{:else if queued}
								<Empty.Title>Sync queued</Empty.Title>
								<Empty.Description>{action.hint}</Empty.Description>
							{:else if registry.syncState === SyncState.NEVER || !registry.lastSyncAt}
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
						{#if !syncing}
							<Empty.Content class="flex-row flex-wrap justify-center">
								{@render syncButton()}
								<Button variant="outline" onclick={() => (tab = 'settings')}>Edit discovery settings</Button>
							</Empty.Content>
						{/if}
					</Empty.Root>
				{/snippet}
			</PaginatedTable>
			{#if packageList?.totalSize}
				<div class="flex justify-end">
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
						packages.refresh();
					}}
				/>
			{/key}
		</Tabs.Content>
	</Tabs.Root>
{/if}
