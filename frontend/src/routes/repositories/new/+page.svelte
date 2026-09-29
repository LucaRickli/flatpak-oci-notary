<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { toast } from 'svelte-sonner';
	import * as Card from '$lib/components/ui/card';
	import * as Empty from '$lib/components/ui/empty';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { Spinner } from '$lib/components/ui/spinner';
	import PageHeader from '$lib/components/page-header.svelte';
	import ErrorAlert from '$lib/components/error-alert.svelte';
	import RepositoryFields, { type RepositoryMeta } from '$lib/components/repository-fields.svelte';
	import SourcesEditor from '$lib/components/sources-editor.svelte';
	import { registryClient, repositoryClient } from '$lib/api';
	import { Resource } from '$lib/resource.svelte';
	import { reportError } from '$lib/session.svelte';
	import { SLUG_PATTERN } from '$lib/format';
	import { canonicalId } from '$lib/ids';
	import { includeEverything, type SourceDraft } from '$lib/sources';
	import ServerIcon from '@lucide/svelte/icons/server';

	const registries = new Resource(async () => (await registryClient.listRegistries({})).registries);

	const registryParam = page.url.searchParams.get('registry');
	let meta = $state<RepositoryMeta>({
		slug: '',
		title: '',
		description: '',
		homepage: '',
		registryId: canonicalId(registryParam) ?? ''
	});
	let sources = $state<SourceDraft[]>([includeEverything()]);
	let saving = $state(false);

	// Preselect the registry when there is only one.
	$effect(() => {
		const list = registries.current;
		if (list?.length === 1 && !meta.registryId) meta.registryId = list[0].id;
	});

	const valid = $derived(meta.title.trim() !== '' && SLUG_PATTERN.test(meta.slug) && meta.registryId !== '');

	async function create(event: SubmitEvent) {
		event.preventDefault();
		if (!valid) return;
		saving = true;
		try {
			const res = await repositoryClient.createRepository({
				repository: { ...$state.snapshot(meta), sources: $state.snapshot(sources) }
			});
			toast.success(`Repository ${meta.title} created`);
			if (res.repository) goto(`/repositories/${res.repository.id}`);
		} catch (err) {
			reportError(err, 'Could not create repository');
		} finally {
			saving = false;
		}
	}
</script>

<PageHeader title="New repository" description="Publish a flatpak remote serving a selection of indexed packages." />

{#if registries.error}
	<ErrorAlert error={registries.error} onretry={registries.refresh} />
{:else if !registries.current}
	<Skeleton class="h-96 rounded-xl" />
{:else if registries.current.length === 0}
	<Empty.Root class="border border-dashed">
		<Empty.Header>
			<Empty.Media variant="icon"><ServerIcon /></Empty.Media>
			<Empty.Title>No registries</Empty.Title>
			<Empty.Description>A repository serves images of one registry. Add a registry first.</Empty.Description>
		</Empty.Header>
		<Empty.Content><Button href="/registries/new">Add registry</Button></Empty.Content>
	</Empty.Root>
{:else}
	<form onsubmit={create} class="flex flex-col gap-6">
		<Card.Root>
			<Card.Header>
				<Card.Title>Details</Card.Title>
				<Card.Description>How the remote is named and where its images come from.</Card.Description>
			</Card.Header>
			<Card.Content>
				<RepositoryFields bind:value={meta} registries={registries.current} autoSlug />
			</Card.Content>
		</Card.Root>
		<Card.Root>
			<Card.Header>
				<Card.Title>Rules</Card.Title>
				<Card.Description>Select which images of the registry this repository serves. You can preview the result after creating it.</Card.Description>
			</Card.Header>
			<Card.Content>
				<SourcesEditor bind:sources registryId={meta.registryId} />
			</Card.Content>
			<Card.Footer class="justify-end gap-2 border-t">
				<Button type="button" variant="ghost" href="/repositories">Cancel</Button>
				<Button type="submit" disabled={saving || !valid}>
					{#if saving}<Spinner data-icon="inline-start" />{/if}
					Create repository
				</Button>
			</Card.Footer>
		</Card.Root>
	</form>
{/if}
