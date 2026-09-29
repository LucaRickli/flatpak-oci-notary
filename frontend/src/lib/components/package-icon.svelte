<script lang="ts">
	import { urls, type Image } from '$lib/api';
	import { imageTitle } from '$lib/format';
	import { cn } from '$lib/utils/shadcn';
	import PackageIcon from '@lucide/svelte/icons/package';

	let {
		image,
		iconId,
		alt,
		class: className
	}: {
		/** Shows this image's icon (when it has one). */
		image?: Pick<Image, 'id' | 'hasIcon' | 'name' | 'flatpakId' | 'repository'>;
		/** Or: the id of an image with an icon (e.g. Package.iconImageId); empty = none. */
		iconId?: string;
		alt?: string;
		class?: string;
	} = $props();

	const src = $derived(iconId ?? (image?.hasIcon ? image.id : ''));
	const label = $derived(alt ?? (image ? imageTitle(image) : ''));

	let failed = $state(false);
	$effect(() => {
		// Retry when a different icon is shown in the same slot.
		void src;
		failed = false;
	});
</script>

<div
	class={cn(
		'flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-md bg-muted text-muted-foreground',
		className
	)}
>
	{#if src && !failed}
		<img
			src={urls.icon(src)}
			alt={label}
			class="size-full object-contain"
			loading="lazy"
			onerror={() => (failed = true)}
		/>
	{:else}
		<PackageIcon class="size-1/2" />
	{/if}
</div>
