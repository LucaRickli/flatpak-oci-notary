<script lang="ts">
	import { embeddedSync } from '$lib/sync';
	import { cn } from '$lib/utils/shadcn';
	import InfoIcon from '@lucide/svelte/icons/info';

	let { class: className }: { class?: string } = $props();
</script>

<!-- Only shown when this server doesn't run syncs itself (GetInfo.embedded_sync false). -->
{#if !embeddedSync()}
	<p class={cn('flex items-start gap-1.5 text-xs text-muted-foreground', className)}>
		<InfoIcon class="mt-px size-3.5 shrink-0" />
		<span>
			Syncing runs in a separate <code class="font-mono">notary sync</code> process. “Request sync” queues a sync
			that it picks up on its next run.
		</span>
	</p>
{/if}
