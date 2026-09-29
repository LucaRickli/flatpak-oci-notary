<script lang="ts">
	import { Spinner } from '$lib/components/ui/spinner';
	import { cn } from '$lib/utils/shadcn';

	let { class: className }: { class?: string } = $props();
</script>

<p class={cn('flex items-center gap-1.5 text-xs text-muted-foreground', className)} role="status">
	<Spinner class="size-3.5" />
	Sync in progress — list is updating
</p>
