<script lang="ts">
	import { goto } from '$app/navigation';
	import * as Table from '$lib/components/ui/table';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { Button } from '$lib/components/ui/button';
	import SyncStateBadge from './sync-state-badge.svelte';
	import SyncProgress from './sync-progress.svelte';
	import type { Registry } from '$lib/api';
	import { embeddedSync, isQueued, isSyncing, syncAction } from '$lib/sync';
	import { formatDate, formatDuration, formatInterval, formatRelative } from '$lib/format';
	import { rowClick } from '$lib/row-link';
	import { cn } from '$lib/utils/shadcn';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import ServerIcon from '@lucide/svelte/icons/server';

	let {
		registries,
		onsync,
		compact = false
	}: {
		registries: Registry[];
		onsync: (registry: Registry) => void;
		/** Fewer columns (dashboard). */
		compact?: boolean;
	} = $props();
</script>

<Table.Root>
	<Table.Header>
		<Table.Row class="hover:bg-transparent">
			<Table.Head class="pl-4">Registry</Table.Head>
			<Table.Head>Status</Table.Head>
			<Table.Head class="hidden text-right sm:table-cell">Images</Table.Head>
			{#if !compact}
				<Table.Head class="hidden text-right lg:table-cell">Repositories</Table.Head>
			{/if}
			{#if !compact}<Table.Head class="hidden md:table-cell">Last sync</Table.Head>{/if}
			{#if !compact}
				<Table.Head class="hidden lg:table-cell">Schedule</Table.Head>
			{/if}
			<Table.Head class="w-12 pr-3"><span class="sr-only">Actions</span></Table.Head>
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#each registries as registry (registry.id)}
			{@const busy = isSyncing(registry) || isQueued(registry)}
			{@const action = syncAction(registry)}
			<Table.Row class="h-16 cursor-pointer" onclick={rowClick(() => goto(`/registries/${registry.id}`))}>
				<Table.Cell class={cn('max-w-44 pl-4', compact ? 'sm:max-w-60' : 'sm:max-w-80')}>
					<div class="flex items-center gap-3">
						<div
							class="hidden size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary sm:flex"
						>
							<ServerIcon class="size-4" />
						</div>
						<div class="flex min-w-0 flex-col">
							<a href="/registries/{registry.id}" class="truncate font-medium hover:underline">{registry.name}</a>
							<span class="truncate font-mono text-xs text-muted-foreground">{registry.url.replace(/^https?:\/\//, '')}</span>
						</div>
					</div>
				</Table.Cell>
				<Table.Cell>
					<div class={cn('flex flex-col gap-1.5', compact ? 'w-28' : 'w-32 sm:w-40')}>
						<SyncStateBadge {registry} />
						<SyncProgress {registry} />
						{#if compact && registry.lastSyncAt && !isSyncing(registry)}
							<span class="text-xs text-muted-foreground" title={formatDate(registry.lastSyncAt)}>
								{formatRelative(registry.lastSyncAt)}
							</span>
						{/if}
					</div>
				</Table.Cell>
				<Table.Cell class="hidden text-right font-medium tabular-nums sm:table-cell">
					{registry.imageCount.toLocaleString()}
				</Table.Cell>
				{#if !compact}
					<Table.Cell class="hidden text-right tabular-nums lg:table-cell">
						{registry.repositoryCount.toLocaleString()}
					</Table.Cell>
				{/if}
				{#if !compact}
					<Table.Cell class="hidden text-muted-foreground md:table-cell">
						<span title={formatDate(registry.lastSyncAt)}>{formatRelative(registry.lastSyncAt)}</span>
						{#if registry.lastSyncAt && registry.lastSyncDurationMs}
							<span class="text-xs"> · {formatDuration(registry.lastSyncDurationMs)}</span>
						{/if}
					</Table.Cell>
				{/if}
				{#if !compact}
					<Table.Cell class="hidden text-muted-foreground lg:table-cell">
						{formatInterval(registry.syncIntervalMinutes)}
					</Table.Cell>
				{/if}
				<Table.Cell class="pr-3 text-right">
					<Tooltip.Root>
						<Tooltip.Trigger>
							{#snippet child({ props })}
								<Button
									{...props}
									variant="ghost"
									size="icon-sm"
									aria-label="{action.label}: {registry.name}"
									disabled={busy}
									onclick={() => onsync(registry)}
								>
									<RefreshCwIcon class={cn(isSyncing(registry) && 'animate-spin')} />
								</Button>
							{/snippet}
						</Tooltip.Trigger>
						<Tooltip.Content class="max-w-xs">
							{busy || embeddedSync() ? action.label : `${action.label}: ${action.hint}`}
						</Tooltip.Content>
					</Tooltip.Root>
				</Table.Cell>
			</Table.Row>
		{/each}
	</Table.Body>
</Table.Root>
