<script lang="ts">
	import { goto } from '$app/navigation';
	import * as Card from '$lib/components/ui/card';
	import * as Table from '$lib/components/ui/table';
	import * as Empty from '$lib/components/ui/empty';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { Button } from '$lib/components/ui/button';
	import PageHeader from '$lib/components/page-header.svelte';
	import SyncStateBadge from '$lib/components/sync-state-badge.svelte';
	import SyncProgress from '$lib/components/sync-progress.svelte';
	import ExternalSyncNote from '$lib/components/external-sync-note.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import TableSkeleton from '$lib/components/table-skeleton.svelte';
	import { registryClient, type Registry } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { poll } from '$lib/poll.svelte';
	import { embeddedSync, isQueued, isSyncing, startSync, syncAction, syncPollInterval } from '$lib/sync';
	import { formatInterval, formatRelative } from '$lib/format';
	import { cn } from '$lib/utils/shadcn';
	import { rowClick } from '$lib/row-link';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import ServerIcon from '@lucide/svelte/icons/server';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';

	const registries = new Resource(async () => (await registryClient.listRegistries({})).registries);

	// Poll while any registry is syncing or waits for a syncer.
	poll(() => syncPollInterval(registries.current), registries.poll);

	async function sync(registry: Registry) {
		const updated = await startSync(registry);
		if (updated && registries.current) {
			registries.current = registries.current.map((r) => (r.id === updated.id ? updated : r));
		}
	}
</script>

<PageHeader title="Registries" description="Upstream OCI registries that get indexed for flatpak images.">
	{#snippet actions()}
		<Button href="/registries/new"><PlusIcon data-icon="inline-start" />Add registry</Button>
	{/snippet}
</PageHeader>

<ExternalSyncNote class="-mt-2" />

{#if registries.error}
	<ErrorAlert error={registries.error} onretry={registries.refresh} />
{:else if !registries.current}
	<Card.Root><Card.Content><TableSkeleton rows={3} /></Card.Content></Card.Root>
{:else if registries.current.length === 0}
	<Empty.Root class="border border-dashed">
		<Empty.Header>
			<Empty.Media variant="icon"><ServerIcon /></Empty.Media>
			<Empty.Title>No registries yet</Empty.Title>
			<Empty.Description>
				Add an OCI registry such as ghcr.io or registry.fedoraproject.org to start indexing flatpaks.
			</Empty.Description>
		</Empty.Header>
		<Empty.Content>
			<Button href="/registries/new"><PlusIcon data-icon="inline-start" />Add registry</Button>
		</Empty.Content>
	</Empty.Root>
{:else}
	<Card.Root class="py-0">
		<Table.Root>
			<Table.Header>
				<Table.Row>
					<Table.Head class="pl-4">Registry</Table.Head>
					<Table.Head>Status</Table.Head>
					<Table.Head class="hidden md:table-cell">Last sync</Table.Head>
					<Table.Head class="hidden text-right sm:table-cell">Images</Table.Head>
					<Table.Head class="hidden text-right lg:table-cell">Repositories</Table.Head>
					<Table.Head class="hidden lg:table-cell">Schedule</Table.Head>
					<Table.Head class="w-12 pr-4"><span class="sr-only">Actions</span></Table.Head>
				</Table.Row>
			</Table.Header>
			<Table.Body>
				{#each registries.current as registry (registry.id)}
					{@const busy = isSyncing(registry) || isQueued(registry)}
					{@const action = syncAction(registry)}
					<Table.Row class="cursor-pointer" onclick={rowClick(() => goto(`/registries/${registry.id}`))}>
						<Table.Cell class="max-w-40 pl-4 sm:max-w-72">
							<div class="flex min-w-0 flex-col">
								<a href="/registries/{registry.id}" class="truncate font-medium hover:underline">{registry.name}</a>
								<span class="truncate font-mono text-xs text-muted-foreground">{registry.url}</span>
							</div>
						</Table.Cell>
						<Table.Cell>
							<div class="flex w-28 flex-col gap-1.5 sm:w-36">
								<SyncStateBadge {registry} />
								<SyncProgress {registry} />
							</div>
						</Table.Cell>
						<Table.Cell class="hidden text-muted-foreground md:table-cell">
							{formatRelative(registry.lastSyncAt)}
						</Table.Cell>
						<Table.Cell class="hidden text-right tabular-nums sm:table-cell">{registry.imageCount}</Table.Cell>
						<Table.Cell class="hidden text-right tabular-nums lg:table-cell">{registry.repositoryCount}</Table.Cell>
						<Table.Cell class="hidden text-muted-foreground lg:table-cell">
							{formatInterval(registry.syncIntervalMinutes)}
						</Table.Cell>
						<Table.Cell class="pr-4">
							<Tooltip.Root>
								<Tooltip.Trigger>
									{#snippet child({ props })}
										<Button
											{...props}
											variant="ghost"
											size="icon-sm"
											aria-label="{action.label}: {registry.name}"
											disabled={busy}
											onclick={(e: MouseEvent) => {
												e.stopPropagation();
												sync(registry);
											}}
										>
											<RefreshCwIcon class={cn(isSyncing(registry) && 'animate-spin')} />
										</Button>
									{/snippet}
								</Tooltip.Trigger>
								<Tooltip.Content class="max-w-xs">{busy || embeddedSync() ? action.label : `${action.label}: ${action.hint}`}</Tooltip.Content>
							</Tooltip.Root>
						</Table.Cell>
					</Table.Row>
				{/each}
			</Table.Body>
		</Table.Root>
	</Card.Root>
{/if}
