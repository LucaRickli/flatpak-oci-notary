<script lang="ts">
	// Stable per-image link: resolves the image and continues on its package page with the image
	// selected. Images without a usable flatpak ref are shown here directly.
	import { goto } from '$app/navigation';
	import * as Empty from '$lib/components/ui/empty';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import PageHeader from '$lib/components/page-header.svelte';
	import PackageIcon from '$lib/components/package-icon.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import ImageDetails from '$lib/components/image-details.svelte';
	import { imageClient } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { isNotFound } from '$lib/session.svelte';
	import { setCrumb } from '$lib/breadcrumb.svelte';
	import { imageTitle } from '$lib/format';
	import { kindSlug, imageHref } from '$lib/links';

	let { data } = $props();

	const resource = new Resource(() => imageClient.getImage({ id: data.id }));
	const current = $derived.by(() => {
		const d = resource.current;
		const image = d?.image;
		return d && image?.id === data.id ? { ...d, image } : undefined;
	});
	const routable = $derived(!!current && !!kindSlug(current.image.kind) && !!current.image.flatpakId);

	$effect(() => {
		if (!current) return;
		if (routable) goto(imageHref(current.image), { replaceState: true });
		else setCrumb(imageTitle(current.image));
	});
</script>

{#if resource.error}
	{#if isNotFound(resource.error)}
		<Empty.Root>
			<Empty.Header>
				<Empty.Title>Image not found</Empty.Title>
				<Empty.Description>It may have been removed from its registry during a sync.</Empty.Description>
			</Empty.Header>
			<Empty.Content><Button href="/packages" variant="outline">Back to packages</Button></Empty.Content>
		</Empty.Root>
	{:else}
		<ErrorAlert error={resource.error} onretry={resource.refresh} />
	{/if}
{:else if !current || routable}
	<div class="flex items-center gap-4">
		<Skeleton class="size-16 shrink-0 rounded-xl" />
		<div class="flex min-w-0 flex-col gap-2">
			<Skeleton class="h-7 w-56 max-w-full" />
			<Skeleton class="h-4 w-80 max-w-full" />
		</div>
	</div>
	<Skeleton class="h-64 rounded-xl" />
{:else}
	<PageHeader title={imageTitle(current.image)} description={current.image.summary}>
		{#snippet media()}
			<PackageIcon image={current.image} class="size-16 rounded-xl" />
		{/snippet}
	</PageHeader>
	<ImageDetails detail={current} />
{/if}
