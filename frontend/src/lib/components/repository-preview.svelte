<script lang="ts">
	import * as Empty from '$lib/components/ui/empty';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import * as InputGroup from '$lib/components/ui/input-group';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Spinner } from '$lib/components/ui/spinner';
	import PaginatedImages from './paginated-images.svelte';
	import MissingRuntimesAlert from './missing-runtimes-alert.svelte';
	import { repositoryClient } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { DEFAULT_PAGE_SIZE, pageRequest } from '$lib/pagination';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';
	import InfoIcon from '@lucide/svelte/icons/info';

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
	<div class="flex flex-wrap items-center gap-2">
		<ToggleGroup.Root
			type="single"
			variant="segmented"
			spacing={0.5}
			size="sm"
			value={architecture}
			onValueChange={(v) => v && (architecture = v)}
			aria-label="Architecture"
		>
			<ToggleGroup.Item value="all">All arches</ToggleGroup.Item>
			<ToggleGroup.Item value="amd64" class="font-mono">amd64</ToggleGroup.Item>
			<ToggleGroup.Item value="arm64" class="font-mono">arm64</ToggleGroup.Item>
		</ToggleGroup.Root>
		<InputGroup.Root class="h-8 w-44">
			<InputGroup.Addon><InputGroup.Text>#</InputGroup.Text></InputGroup.Addon>
			<InputGroup.Input bind:value={tag} placeholder="all tags" class="font-mono" aria-label="Tag" />
			<InputGroup.Addon align="inline-end">
				{#if preview.loading && preview.current}
					<Spinner />
				{:else}
					<Tooltip.Root>
						<Tooltip.Trigger>
							{#snippet child({ props })}
								<InputGroup.Button {...props} size="icon-xs" aria-label="About tags"><InfoIcon /></InputGroup.Button>
							{/snippet}
						</Tooltip.Trigger>
						<Tooltip.Content class="max-w-xs">
							Flatpak requests tag=latest unless the remote URL ends with #&lt;tag&gt;. Clear the tag to see images of
							all tags. The preview reflects the saved rules.
						</Tooltip.Content>
					</Tooltip.Root>
				{/if}
			</InputGroup.Addon>
		</InputGroup.Root>
		{#if !preview.error && total > 0}
			<p class="ml-auto text-sm text-muted-foreground">
				<span class="font-medium text-foreground tabular-nums">{total.toLocaleString()}</span>
				{total === 1 ? 'image' : 'images'} served
			</p>
		{/if}
	</div>

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
