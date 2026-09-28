<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import * as Command from '$lib/components/ui/command';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Button } from '$lib/components/ui/button';
	import { Spinner } from '$lib/components/ui/spinner';
	import PackageIcon from './package-icon.svelte';
	import KindBadge from './kind-badge.svelte';
	import ErrorAlert from './error-alert.svelte';
	import { imageClient, type Image } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { imageTitle } from '$lib/format';
	import type { SourceDraft } from '$lib/sources';

	let {
		open = $bindable(false),
		registryId,
		sources,
		onpick
	}: {
		open?: boolean;
		registryId: number;
		/** Current rules, to mark packages that already have a rule. */
		sources: SourceDraft[];
		onpick: (rule: SourceDraft) => void;
	} = $props();

	let mode = $state<'include' | 'exclude'>('include');

	// Loaded up front so the list (and its search input) is ready when the dialog opens.
	const images = new Resource(async () =>
		registryId ? (await imageClient.listImages({ registryId })).images : []
	);

	// Refresh in the background on every open, in case a sync added packages.
	$effect(() => {
		if (open) images.refresh();
	});

	interface PackageEntry {
		repository: string;
		/** Representative image (prefers one with an icon). */
		image: Image;
		arches: string[];
		count: number;
	}

	// One entry per OCI repository; a rule selects all of its arches and tags.
	const packages = $derived.by(() => {
		const byRepo = new Map<string, PackageEntry>();
		for (const img of images.current ?? []) {
			let entry = byRepo.get(img.repository);
			if (!entry) {
				entry = { repository: img.repository, image: img, arches: [], count: 0 };
				byRepo.set(img.repository, entry);
			}
			if (!entry.image.hasIcon && img.hasIcon) entry.image = img;
			if (!entry.arches.includes(img.architecture)) entry.arches.push(img.architecture);
			entry.count++;
		}
		return [...byRepo.values()].sort((a, b) => imageTitle(a.image).localeCompare(imageTitle(b.image)));
	});

	function hasRule(repository: string) {
		return sources.some((s) => s.repositoryPattern === repository && s.exclude === (mode === 'exclude'));
	}

	function pick(entry: PackageEntry) {
		if (hasRule(entry.repository)) return;
		onpick({ repositoryPattern: entry.repository, refPattern: '*', tagPattern: '*', exclude: mode === 'exclude' });
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="gap-0 p-0 sm:max-w-xl">
		<Dialog.Header class="p-4 pb-3">
			<Dialog.Title>Add package</Dialog.Title>
			<Dialog.Description>
				Adds a rule matching the package's OCI repository exactly, with all its refs, arches and tags.
			</Dialog.Description>
		</Dialog.Header>
		{#if images.error}
			<div class="px-4 pb-4"><ErrorAlert error={images.error} onretry={images.refresh} /></div>
		{:else if !images.current}
			<div class="flex justify-center p-8"><Spinner class="size-5 text-muted-foreground" /></div>
		{:else}
			<Command.Root class="rounded-none! border-t">
				<Command.Input placeholder="Search packages…" />
				<Command.List class="max-h-80">
					<Command.Empty>
						{packages.length === 0 ? 'No packages indexed for this registry yet.' : 'No packages found.'}
					</Command.Empty>
					<Command.Group heading="{packages.length} packages">
						{#each packages as entry (entry.repository)}
							{@const added = hasRule(entry.repository)}
							<Command.Item
								value={entry.repository}
								keywords={[entry.image.name, entry.image.flatpakId]}
								data-checked={added}
								onSelect={() => pick(entry)}
							>
								<PackageIcon image={entry.image} class="size-8" />
								<div class="flex min-w-0 flex-1 flex-col">
									<span class="truncate font-medium">{imageTitle(entry.image)}</span>
									<span class="truncate font-mono text-xs text-muted-foreground">{entry.repository}</span>
								</div>
								<span class="hidden text-xs text-muted-foreground sm:inline">{entry.arches.join(', ')}</span>
								<KindBadge kind={entry.image.kind} />
							</Command.Item>
						{/each}
					</Command.Group>
				</Command.List>
			</Command.Root>
		{/if}
		<Dialog.Footer class="m-0 flex-row items-center justify-between p-3 sm:justify-between">
			<ToggleGroup.Root
				type="single"
				size="sm"
				variant="outline"
				value={mode}
				onValueChange={(v) => v && (mode = v as 'include' | 'exclude')}
				aria-label="Rule type"
			>
				<ToggleGroup.Item value="include">Include</ToggleGroup.Item>
				<ToggleGroup.Item value="exclude">Exclude</ToggleGroup.Item>
			</ToggleGroup.Root>
			<Button onclick={() => (open = false)}>Done</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
