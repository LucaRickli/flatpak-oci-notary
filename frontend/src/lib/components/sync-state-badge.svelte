<script lang="ts">
	import { Badge } from '$lib/components/ui/badge';
	import { Spinner } from '$lib/components/ui/spinner';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { SyncState, type Registry } from '$lib/api';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import ClockIcon from '@lucide/svelte/icons/clock';

	let { registry }: { registry: Pick<Registry, 'syncState' | 'lastSyncError'> } = $props();
</script>

{#if registry.syncState === SyncState.SYNCING}
	<Badge variant="secondary"><Spinner data-icon="inline-start" />Syncing</Badge>
{:else if registry.syncState === SyncState.OK}
	<Badge variant="outline"><CircleCheckIcon data-icon="inline-start" />Synced</Badge>
{:else if registry.syncState === SyncState.ERROR}
	<Tooltip.Root>
		<Tooltip.Trigger>
			{#snippet child({ props })}
				<Badge variant="destructive" {...props}><CircleAlertIcon data-icon="inline-start" />Error</Badge>
			{/snippet}
		</Tooltip.Trigger>
		<Tooltip.Content class="max-w-sm">{registry.lastSyncError || 'Sync failed'}</Tooltip.Content>
	</Tooltip.Root>
{:else}
	<Badge variant="secondary"><ClockIcon data-icon="inline-start" />Never synced</Badge>
{/if}
