<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Empty from '$lib/components/ui/empty';
	import * as Field from '$lib/components/ui/field';
	import * as InputGroup from '$lib/components/ui/input-group';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Spinner } from '$lib/components/ui/spinner';
	import ImagesTable from './images-table.svelte';
	import ErrorAlert from './error-alert.svelte';
	import TableSkeleton from './table-skeleton.svelte';
	import { RefKind, repositoryClient } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';

	let {
		repositoryId,
		revision = 0
	}: {
		repositoryId: number;
		/** Bump to reload after the repository was saved. */
		revision?: number;
	} = $props();

	let architecture = $state('all');
	let tag = $state('latest');
	let debouncedTag = $state('latest');

	$effect(() => {
		const t = tag.trim();
		const timer = setTimeout(() => (debouncedTag = t), 300);
		return () => clearTimeout(timer);
	});

	const preview = new Resource(async () => {
		void revision;
		const res = await repositoryClient.previewRepository({
			id: repositoryId,
			architecture: architecture === 'all' ? '' : architecture,
			tag: debouncedTag
		});
		return res.images;
	});

	const counts = $derived.by(() => {
		const images = preview.current ?? [];
		return {
			refs: new Set(images.map((i) => i.ref)).size,
			apps: images.filter((i) => i.kind === RefKind.APP).length,
			runtimes: images.filter((i) => i.kind === RefKind.RUNTIME).length
		};
	});
</script>

<div class="flex flex-col gap-4">
	<div class="flex flex-col gap-4 sm:flex-row sm:items-end">
		<Field.Field class="w-auto">
			<Field.FieldLabel id="preview-arch">Architecture</Field.FieldLabel>
			<ToggleGroup.Root
				type="single"
				variant="outline"
				value={architecture}
				onValueChange={(v) => v && (architecture = v)}
				aria-labelledby="preview-arch"
			>
				<ToggleGroup.Item value="all">All</ToggleGroup.Item>
				<ToggleGroup.Item value="amd64">amd64</ToggleGroup.Item>
				<ToggleGroup.Item value="arm64">arm64</ToggleGroup.Item>
			</ToggleGroup.Root>
		</Field.Field>
		<Field.Field class="sm:max-w-56">
			<Field.FieldLabel for="preview-tag">Tag</Field.FieldLabel>
			<InputGroup.Root>
				<InputGroup.Addon><InputGroup.Text>#</InputGroup.Text></InputGroup.Addon>
				<InputGroup.Input id="preview-tag" bind:value={tag} placeholder="all tags" class="font-mono" />
				{#if preview.loading && preview.current}
					<InputGroup.Addon align="inline-end"><Spinner /></InputGroup.Addon>
				{/if}
			</InputGroup.Root>
		</Field.Field>
	</div>
	<p class="text-xs text-muted-foreground">
		Flatpak requests <code class="font-mono">tag=latest</code> unless the remote URL ends with
		<code class="font-mono">#&lt;tag&gt;</code>. Clear the tag to see images of all tags. The preview reflects the saved
		rules.
	</p>

	{#if preview.error}
		<ErrorAlert error={preview.error} onretry={preview.refresh} />
	{:else if !preview.current}
		<Card.Root><Card.Content><TableSkeleton /></Card.Content></Card.Root>
	{:else if preview.current.length === 0}
		<Empty.Root class="border border-dashed">
			<Empty.Header>
				<Empty.Media variant="icon"><EyeOffIcon /></Empty.Media>
				<Empty.Title>Nothing served</Empty.Title>
				<Empty.Description>
					No image matches the rules{debouncedTag ? ` with tag “${debouncedTag}”` : ''}{architecture !== 'all'
						? ` for ${architecture}`
						: ''}. Adjust the rules or the filters above.
				</Empty.Description>
			</Empty.Header>
		</Empty.Root>
	{:else}
		<p class="text-sm">
			<span class="font-medium">{preview.current.length}</span>
			{preview.current.length === 1 ? 'image' : 'images'} ·
			{counts.refs}
			{counts.refs === 1 ? 'ref' : 'refs'} · {counts.apps}
			{counts.apps === 1 ? 'app' : 'apps'} · {counts.runtimes}
			{counts.runtimes === 1 ? 'runtime' : 'runtimes'}
		</p>
		<Card.Root class="py-0">
			<ImagesTable images={preview.current} showRegistry={false} />
		</Card.Root>
	{/if}
</div>
