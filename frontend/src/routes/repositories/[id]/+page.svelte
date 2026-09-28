<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import type { MessageInitShape } from '@bufbuild/protobuf';
	import * as Card from '$lib/components/ui/card';
	import * as Tabs from '$lib/components/ui/tabs';
	import * as Alert from '$lib/components/ui/alert';
	import * as Empty from '$lib/components/ui/empty';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { Spinner } from '$lib/components/ui/spinner';
	import PageHeader from '$lib/components/page-header.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import ConfirmDelete from '$lib/components/confirm-delete.svelte';
	import RepositoryUsage from '$lib/components/repository-usage.svelte';
	import RepositoryPreview from '$lib/components/repository-preview.svelte';
	import RepositoryFields, { type RepositoryMeta } from '$lib/components/repository-fields.svelte';
	import SourcesEditor from '$lib/components/sources-editor.svelte';
	import StatCard from '$lib/components/stat-card.svelte';
	import {
		registryClient,
		repositoryClient,
		type Repository,
		type RepositoryInputSchema
	} from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { isNotFound, reportError } from '$lib/session.svelte';
	import { setCrumb } from '$lib/breadcrumb.svelte';
	import { formatDate, formatRelative, SLUG_PATTERN } from '$lib/format';
	import { sameSources, toDraft, type SourceDraft } from '$lib/sources';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import ServerIcon from '@lucide/svelte/icons/server';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';

	let { data } = $props();

	const resource = new Resource(async () => (await repositoryClient.getRepository({ id: data.id })).repository);
	const registries = new Resource(async () => (await registryClient.listRegistries({})).registries);
	const repository = $derived(resource.current?.id === data.id ? resource.current : undefined);

	const TABS = ['overview', 'rules', 'preview', 'settings'];
	const initialTab = page.url.searchParams.get('tab') ?? '';
	let tab = $state(TABS.includes(initialTab) ? initialTab : 'overview');
	let revision = $state(0);
	let saving = $state(false);

	// Editable drafts, reset whenever a (new or saved) repository arrives.
	let sources = $state<SourceDraft[]>([]);
	let meta = $state<RepositoryMeta>({ slug: '', title: '', description: '', homepage: '', registryId: 0 });

	function metaOf(r: Repository): RepositoryMeta {
		return {
			slug: r.slug,
			title: r.title,
			description: r.description,
			homepage: r.homepage,
			registryId: r.registryId
		};
	}

	$effect(() => {
		const r = repository;
		if (!r) return;
		untrack(() => {
			sources = r.sources.map(toDraft);
			meta = metaOf(r);
		});
	});

	$effect(() => {
		if (repository) setCrumb(repository.title || repository.slug);
	});

	const rulesDirty = $derived(!!repository && !sameSources(sources, repository.sources.map(toDraft)));
	const metaDirty = $derived(!!repository && JSON.stringify(meta) !== JSON.stringify(metaOf(repository)));
	const metaValid = $derived(meta.title.trim() !== '' && SLUG_PATTERN.test(meta.slug) && meta.registryId > 0);

	async function save(patch: Partial<RepositoryMeta> & { sources?: SourceDraft[] }): Promise<boolean> {
		const r = repository;
		if (!r) return false;
		const input: MessageInitShape<typeof RepositoryInputSchema> = {
			...metaOf(r),
			sources: r.sources.map(toDraft),
			...patch
		};
		saving = true;
		try {
			const res = await repositoryClient.updateRepository({ id: r.id, repository: input });
			resource.current = res.repository;
			revision++;
			return true;
		} catch (err) {
			reportError(err, 'Could not save repository');
			return false;
		} finally {
			saving = false;
		}
	}

	async function saveRules() {
		if (await save({ sources: $state.snapshot(sources) })) {
			toast.success('Rules saved', {
				description: `${resource.current?.imageCount ?? 0} images matched`,
				action: { label: 'Preview', onClick: () => (tab = 'preview') }
			});
		}
	}

	async function saveSettings(event: SubmitEvent) {
		event.preventDefault();
		if (!metaValid) return;
		if (await save($state.snapshot(meta))) toast.success('Repository saved');
	}

	async function remove() {
		if (!repository) return;
		try {
			await repositoryClient.deleteRepository({ id: repository.id });
			toast.success(`Repository ${repository.title || repository.slug} deleted`);
			goto('/repositories');
		} catch (err) {
			reportError(err, 'Could not delete repository');
			return false;
		}
	}
</script>

{#if resource.error}
	{#if isNotFound(resource.error)}
		<Empty.Root>
			<Empty.Header>
				<Empty.Title>Repository not found</Empty.Title>
				<Empty.Description>It may have been deleted.</Empty.Description>
			</Empty.Header>
			<Empty.Content><Button href="/repositories" variant="outline">Back to repositories</Button></Empty.Content>
		</Empty.Root>
	{:else}
		<ErrorAlert error={resource.error} onretry={resource.refresh} />
	{/if}
{:else if !repository}
	<div class="flex flex-col gap-2">
		<Skeleton class="h-8 w-64" />
		<Skeleton class="h-4 w-96" />
	</div>
	<Skeleton class="h-72 rounded-xl" />
{:else}
	<PageHeader title={repository.title || repository.slug} description={repository.description}>
		<div class="flex flex-wrap items-center gap-1.5 pt-1">
			<Badge variant="outline" class="font-mono">{repository.slug}</Badge>
			<Badge variant="secondary" href="/registries/{repository.registryId}">
				<ServerIcon data-icon="inline-start" />{repository.registryName}
			</Badge>
		</div>
		{#snippet actions()}
			<ConfirmDelete
				title="Delete {repository.title || repository.slug}?"
				description="The remote stops working for everyone who added it. Images on the registry are not touched."
				onconfirm={remove}
			/>
		{/snippet}
	</PageHeader>

	<Tabs.Root bind:value={tab}>
		<Tabs.List>
			<Tabs.Trigger value="overview">Overview</Tabs.Trigger>
			<Tabs.Trigger value="rules">
				Rules
				{#if rulesDirty}<span class="size-1.5 rounded-full bg-primary" aria-label="unsaved changes"></span>{/if}
			</Tabs.Trigger>
			<Tabs.Trigger value="preview">Preview</Tabs.Trigger>
			<Tabs.Trigger value="settings">Settings</Tabs.Trigger>
		</Tabs.List>

		<Tabs.Content value="overview" class="flex flex-col gap-4 pt-2">
			<div class="grid gap-4 sm:grid-cols-3">
				<StatCard label="Images matched" value={repository.imageCount}>All arches and tags, before per-ref de-duplication</StatCard>
				<StatCard label="Rules" value={repository.sources.length}>
					{repository.sources.filter((s) => s.exclude).length} exclude
				</StatCard>
				<StatCard label="Last updated" value={formatRelative(repository.updatedAt)}>
					Created {formatDate(repository.createdAt)}
				</StatCard>
			</div>
			<RepositoryUsage {repository} />
			{#if repository.homepage}
				<div>
					<Button variant="link" href={repository.homepage} target="_blank" rel="external noopener" class="px-0">
						<ExternalLinkIcon data-icon="inline-start" />{repository.homepage}
					</Button>
				</div>
			{/if}
		</Tabs.Content>

		<Tabs.Content value="rules" class="pt-2">
			<Card.Root>
				<Card.Header>
					<Card.Title>Rules</Card.Title>
					<Card.Description>Which images of {repository.registryName} this repository serves.</Card.Description>
				</Card.Header>
				<Card.Content>
					<SourcesEditor bind:sources registryId={repository.registryId} />
				</Card.Content>
				<Card.Footer class="justify-end gap-2 border-t">
					<Button variant="ghost" disabled={!rulesDirty || saving} onclick={() => (sources = repository.sources.map(toDraft))}>
						Discard changes
					</Button>
					<Button disabled={!rulesDirty || saving} onclick={saveRules}>
						{#if saving}<Spinner data-icon="inline-start" />{/if}
						Save rules
					</Button>
				</Card.Footer>
			</Card.Root>
		</Tabs.Content>

		<Tabs.Content value="preview" class="pt-2">
			{#if tab === 'preview'}
				{#if rulesDirty}
					<Alert.Root class="mb-4">
						<TriangleAlertIcon />
						<Alert.Title>Unsaved rule changes</Alert.Title>
						<Alert.Description>The preview shows the saved rules. Save the rules to preview your changes.</Alert.Description>
					</Alert.Root>
				{/if}
				<RepositoryPreview repositoryId={repository.id} {revision} />
			{/if}
		</Tabs.Content>

		<Tabs.Content value="settings" class="pt-2">
			<form onsubmit={saveSettings}>
				<Card.Root>
					<Card.Header>
						<Card.Title>Settings</Card.Title>
						<Card.Description>Name, description and the registry this repository serves images from.</Card.Description>
					</Card.Header>
					<Card.Content class="flex flex-col gap-4">
						<RepositoryFields bind:value={meta} registries={registries.current ?? []} />
						{#if meta.slug !== repository.slug}
							<Alert.Root>
								<TriangleAlertIcon />
								<Alert.Title>Changing the slug changes the remote URL</Alert.Title>
								<Alert.Description>Machines that already added this remote must add it again.</Alert.Description>
							</Alert.Root>
						{/if}
						{#if meta.registryId !== repository.registryId}
							<Alert.Root>
								<TriangleAlertIcon />
								<Alert.Title>Switching registries</Alert.Title>
								<Alert.Description>
									Rules are kept as they are; rules naming specific repositories may no longer match anything.
								</Alert.Description>
							</Alert.Root>
						{/if}
					</Card.Content>
					<Card.Footer class="justify-end gap-2 border-t">
						<Button type="button" variant="ghost" disabled={!metaDirty || saving} onclick={() => (meta = metaOf(repository))}>
							Discard changes
						</Button>
						<Button type="submit" disabled={!metaDirty || !metaValid || saving}>
							{#if saving}<Spinner data-icon="inline-start" />{/if}
							Save changes
						</Button>
					</Card.Footer>
				</Card.Root>
			</form>
		</Tabs.Content>
	</Tabs.Root>
{/if}
