<script lang="ts">
	import { Progress } from '$lib/components/ui/progress';
	import type { Registry } from '$lib/api';
	import { isSyncing } from '$lib/sync';
	import { cn } from '$lib/utils/shadcn';

	let {
		registry,
		class: className
	}: {
		registry: Pick<Registry, 'syncState' | 'syncRequested' | 'syncRepositoriesDone' | 'syncRepositoriesTotal'>;
		class?: string;
	} = $props();

	const total = $derived(registry.syncRepositoriesTotal);
	const done = $derived(Math.min(registry.syncRepositoriesDone, total));
</script>

<!-- Renders nothing unless a sync is running. -->
{#if isSyncing(registry)}
	<div class={cn('flex min-w-0 flex-col gap-1', className)}>
		{#if total > 0}
			<Progress value={done} max={total} aria-label="Sync progress" />
			<span class="truncate text-xs text-muted-foreground tabular-nums">
				{done.toLocaleString()} / {total.toLocaleString()}
				{total === 1 ? 'repository' : 'repositories'}
			</span>
		{:else}
			<!-- Total unknown yet: a pulsing full bar instead of a misleading 0%. -->
			<Progress
				value={100}
				aria-label="Sync progress"
				aria-valuetext="Discovering repositories"
				class="animate-pulse [&>[data-slot=progress-indicator]]:opacity-40"
			/>
			<span class="truncate text-xs text-muted-foreground">Discovering repositories…</span>
		{/if}
	</div>
{/if}
