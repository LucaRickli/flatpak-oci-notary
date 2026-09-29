<script lang="ts">
	// Call to action of a package page: the `flatpak install` command for the repositories that
	// can serve the selected variant (those publishing its registry).
	import * as Card from '$lib/components/ui/card';
	import * as Select from '$lib/components/ui/select';
	import * as Popover from '$lib/components/ui/popover';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import CodeBlock from './code-block.svelte';
	import CopyButton from './copy-button.svelte';
	import { RefKind, repositoryClient, type Image, type Package } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { cn } from '$lib/utils/shadcn';
	import { remoteAddCommand } from '$lib/remote';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import InfoIcon from '@lucide/svelte/icons/info';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import LibraryIcon from '@lucide/svelte/icons/library';

	let { pkg, variant, class: className }: { pkg: Package; variant: Image | undefined; class?: string } = $props();

	const repositories = new Resource(async () => (await repositoryClient.listRepositories({})).repositories);
	const registryId = $derived(variant?.registryId ?? pkg.registries[0]?.id ?? '');
	const candidates = $derived((repositories.current ?? []).filter((r) => r.registryId === registryId));

	let chosen = $state('');
	const repo = $derived(candidates.find((r) => r.id === chosen) ?? candidates[0]);

	// A branch suffix when the id alone would be ambiguous (runtimes, several branches).
	const target = $derived.by(() => {
		const branch = variant?.branch;
		const needsBranch = !!branch && (pkg.kind === RefKind.RUNTIME || pkg.branches.length > 1);
		return needsBranch ? `${pkg.flatpakId}//${branch}` : pkg.flatpakId;
	});
	const installCmd = $derived(repo ? `flatpak install ${repo.slug} ${target}` : '');
	const remoteAdd = $derived(
		repo?.urls?.remote ? remoteAddCommand(repo) : ''
	);
</script>

<Card.Root class={cn('gap-3 bg-primary/4 ring-primary/20', className)}>
	<Card.Header class="flex items-center gap-2">
		<DownloadIcon class="size-4 text-primary" />
		<Card.Title class="flex-1">Install</Card.Title>
		<Tooltip.Root>
			<Tooltip.Trigger>
				{#snippet child({ props })}
					<Button {...props} variant="ghost" size="icon-xs" aria-label="About installing"><InfoIcon /></Button>
				{/snippet}
			</Tooltip.Trigger>
			<Tooltip.Content class="max-w-xs">
				Repositories publishing images of {variant?.registryName || 'this registry'}. Their rules decide what is
				served: check a repository’s Preview if the install fails.
			</Tooltip.Content>
		</Tooltip.Root>
	</Card.Header>
	<Card.Content class="flex flex-col gap-2.5">
		{#if !repositories.current && !repositories.error}
			<Skeleton class="h-10 rounded-lg" />
		{:else if repo}
			{#if candidates.length > 1}
				<Select.Root type="single" value={repo.id} onValueChange={(v) => (chosen = v)}>
					<Select.Trigger size="sm" class="w-full" aria-label="Repository">
						<LibraryIcon /><span class="truncate">{repo.title || repo.slug}</span>
					</Select.Trigger>
					<Select.Content>
						<Select.Group>
							{#each candidates as r (r.id)}
								<Select.Item value={r.id}>{r.title || r.slug}</Select.Item>
							{/each}
						</Select.Group>
					</Select.Content>
				</Select.Root>
			{/if}
			<CodeBlock code={installCmd} wrap class="bg-background" />
			<div class="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
				<a href="/repositories/{repo.id}" class="flex min-w-0 items-center gap-1 hover:text-foreground">
					<LibraryIcon class="size-3.5 shrink-0" /><span class="truncate">{repo.title || repo.slug}</span>
				</a>
				{#if remoteAdd}
					<Popover.Root>
						<Popover.Trigger>
							{#snippet child({ props })}
								<Button {...props} variant="link" size="xs" class="h-auto px-0">Add the remote first</Button>
							{/snippet}
						</Popover.Trigger>
						<Popover.Content class="w-[min(28rem,calc(100vw-2rem))]" align="end">
							<div class="flex flex-col gap-2">
								<p class="text-sm font-medium">Add {repo.slug} as a remote</p>
								<CodeBlock code={remoteAdd} wrap />
							</div>
						</Popover.Content>
					</Popover.Root>
				{/if}
			</div>
		{:else}
			<p class="text-sm text-muted-foreground">Not published by a repository yet.</p>
			<div class="flex flex-wrap items-center gap-2">
				<Button size="sm" href="/repositories/new?registry={registryId}">
					<PlusIcon data-icon="inline-start" />New repository
				</Button>
				<CopyButton value={pkg.flatpakId} label="Copy ID" showLabel variant="outline" />
			</div>
		{/if}
	</Card.Content>
</Card.Root>
