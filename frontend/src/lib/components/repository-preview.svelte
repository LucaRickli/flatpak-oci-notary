<script lang="ts">
	import * as Empty from '$lib/components/ui/empty';
	import * as Field from '$lib/components/ui/field';
	import * as InputGroup from '$lib/components/ui/input-group';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Spinner } from '$lib/components/ui/spinner';
	import PaginatedImages from './paginated-images.svelte';
	import MissingRuntimesAlert from './missing-runtimes-alert.svelte';
	import { repositoryClient } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { DEFAULT_PAGE_SIZE, pageRequest } from '$lib/pagination';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';

	let {
		repositoryId,
		registryName,
		revision = 0
	}: {
		repositoryId: string;
		/** Registry the repository serves images of, for hints. */
		registryName?: string;
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

	// Overridable; goes back to the first page whenever the filters change.
	let page = $derived.by(() => {
		void architecture;
		void debouncedTag;
		return 1;
	});
	let pageSize = $state(DEFAULT_PAGE_SIZE);

	const preview = new Resource(() => {
		void revision;
		return repositoryClient.previewRepository({
			id: repositoryId,
			architecture: architecture === 'all' ? '' : architecture,
			tag: debouncedTag,
			...pageRequest(page, pageSize)
		});
	});
	const total = $derived(preview.current?.totalSize ?? 0);
	// Computed by the server over the whole selection, not just this page.
	const missing = $derived(preview.current?.missingRuntimes ?? []);
	const missingByRuntime = $derived(new Map(missing.map((m) => [m.runtime, new Set(m.neededBy)])));
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

	{#if !preview.error && total > 0}
		<p class="text-sm">
			<span class="font-medium tabular-nums">{total.toLocaleString()}</span>
			{total === 1 ? 'image' : 'images'} served
		</p>
	{/if}
	{#if !preview.error && missing.length > 0}
		<MissingRuntimesAlert {missing} {registryName} />
	{/if}
	<PaginatedImages
		result={preview.current}
		loading={preview.loading}
		error={preview.error}
		onretry={preview.refresh}
		bind:page
		bind:pageSize
		showRegistry={false}
		missingRuntimes={missingByRuntime}
	>
		{#snippet empty()}
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
		{/snippet}
	</PaginatedImages>
</div>
