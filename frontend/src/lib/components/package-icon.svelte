<script lang="ts">
	import { urls, type Image } from '$lib/api';
	import { imageTitle } from '$lib/format';
	import { cn } from '$lib/utils/shadcn';
	import PackageIcon from '@lucide/svelte/icons/package';

	let {
		image,
		class: className
	}: { image: Pick<Image, 'id' | 'hasIcon' | 'name' | 'flatpakId' | 'repository'>; class?: string } =
		$props();

	let failed = $state(false);
	$effect(() => {
		// Retry when a different image is shown in the same slot.
		void image.id;
		failed = false;
	});
</script>

<div
	class={cn(
		'flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-md bg-muted text-muted-foreground',
		className
	)}
>
	{#if image.hasIcon && !failed}
		<img
			src={urls.icon(image.id)}
			alt={imageTitle(image)}
			class="size-full object-contain"
			loading="lazy"
			onerror={() => (failed = true)}
		/>
	{:else}
		<PackageIcon class="size-1/2" />
	{/if}
</div>
