<script lang="ts">
	import { Skeleton } from '$lib/components/ui/skeleton';

	let { rows = 5 }: { rows?: number } = $props();
</script>

<div class="flex flex-col gap-3">
	{#each { length: rows }, i (i)}
		<div class="flex items-center gap-3">
			<Skeleton class="size-9 rounded-md" />
			<div class="flex flex-1 flex-col gap-1.5">
				<Skeleton class="h-4 w-1/3" />
				<Skeleton class="h-3 w-1/2" />
			</div>
			<Skeleton class="hidden h-4 w-20 md:block" />
		</div>
	{/each}
</div>
