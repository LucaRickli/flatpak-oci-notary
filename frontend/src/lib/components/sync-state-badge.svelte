<script lang="ts">
	import { Badge } from '$lib/components/ui/badge';
	import { Spinner } from '$lib/components/ui/spinner';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { SyncState, type Registry } from '$lib/api';
	import { isQueued, syncAction } from '$lib/sync';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import CircleDashedIcon from '@lucide/svelte/icons/circle-dashed';
	import HourglassIcon from '@lucide/svelte/icons/hourglass';

	let {
		registry
	}: { registry: Pick<Registry, 'syncState' | 'syncRequested' | 'lastSyncError'> } = $props();
</script>

{#if registry.syncState === SyncState.SYNCING}
	<Badge variant="info"><Spinner data-icon="inline-start" />Syncing</Badge>
{:else if isQueued(registry)}
	<Tooltip.Root>
		<Tooltip.Trigger>
			{#snippet child({ props })}
				<Badge variant="warning" {...props}><HourglassIcon data-icon="inline-start" />Queued</Badge>
			{/snippet}
		</Tooltip.Trigger>
		<Tooltip.Content class="max-w-xs">{syncAction(registry).hint}</Tooltip.Content>
	</Tooltip.Root>
{:else if registry.syncState === SyncState.OK}
	<Badge variant="success"><CircleCheckIcon data-icon="inline-start" />Synced</Badge>
{:else if registry.syncState === SyncState.ERROR}
	<Tooltip.Root>
		<Tooltip.Trigger>
			{#snippet child({ props })}
				<Badge variant="destructive" {...props}><CircleAlertIcon data-icon="inline-start" />Failed</Badge>
			{/snippet}
		</Tooltip.Trigger>
		<Tooltip.Content class="max-w-sm">{registry.lastSyncError || 'Sync failed'}</Tooltip.Content>
	</Tooltip.Root>
{:else}
	<Badge variant="muted"><CircleDashedIcon data-icon="inline-start" />Never synced</Badge>
{/if}
