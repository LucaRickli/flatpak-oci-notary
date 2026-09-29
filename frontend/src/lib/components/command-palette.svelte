<script lang="ts">
	// Global ⌘K / Ctrl+K palette: jump to pages, packages (server search), registries and repositories.
	import { afterNavigate } from '$app/navigation';
	import * as Command from '$lib/components/ui/command';
	import { Spinner } from '$lib/components/ui/spinner';
	import PackageIcon from './package-icon.svelte';
	import KindBadge from './kind-badge.svelte';
	import {
		imageClient,
		registryClient,
		repositoryClient,
		type Package,
		type Registry,
		type Repository
	} from '$lib/api';
	import { packageHref } from '$lib/links';
	import { packageTitle } from '$lib/format';
	import { palette } from '$lib/palette.svelte';
	import { NAV_ITEMS } from '$lib/nav';
	import ServerIcon from '@lucide/svelte/icons/server';
	import LibraryIcon from '@lucide/svelte/icons/library';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import SearchIcon from '@lucide/svelte/icons/search';

	let query = $state('');
	let packages = $state.raw<Package[]>([]);
	let searching = $state(false);
	let registries = $state.raw<Registry[]>([]);
	let repositories = $state.raw<Repository[]>([]);

	function onkeydown(e: KeyboardEvent) {
		if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
			e.preventDefault();
			palette.open = !palette.open;
		}
	}

	afterNavigate(() => {
		palette.open = false;
	});

	// Small lists: (re)load them whenever the palette opens.
	$effect(() => {
		if (!palette.open) return;
		query = '';
		registryClient.listRegistries({}).then((r) => (registries = r.registries), () => {});
		repositoryClient.listRepositories({}).then((r) => (repositories = r.repositories), () => {});
	});

	// Debounced server-side package search; the first results show without typing.
	let seq = 0;
	$effect(() => {
		if (!palette.open) return;
		const q = query.trim();
		const mine = ++seq;
		const timer = setTimeout(
			async () => {
				searching = true;
				try {
					const res = await imageClient.listPackages({ query: q, pageSize: q ? 8 : 5 });
					if (mine === seq) packages = res.packages;
				} catch {
					if (mine === seq) packages = [];
				} finally {
					if (mine === seq) searching = false;
				}
			},
			q ? 150 : 0
		);
		return () => clearTimeout(timer);
	});

	const needle = $derived(query.trim().toLowerCase());
	const matches = (...values: string[]) => !needle || values.some((v) => v.toLowerCase().includes(needle));

	const pages = $derived(NAV_ITEMS.filter((item) => matches(item.title)));
	const actions = $derived(
		[
			{ title: 'Add registry', href: '/registries/new' },
			{ title: 'New repository', href: '/repositories/new' }
		].filter((a) => matches(a.title))
	);
	const foundRegistries = $derived(registries.filter((r) => matches(r.name, r.url)).slice(0, 5));
	const foundRepositories = $derived(repositories.filter((r) => matches(r.title, r.slug)).slice(0, 5));
	const nothing = $derived(
		!searching &&
			packages.length + pages.length + actions.length + foundRegistries.length + foundRepositories.length === 0
	);
</script>

<svelte:window {onkeydown} />

<Command.Dialog
	bind:open={palette.open}
	shouldFilter={false}
	title="Search"
	description="Jump to a package, registry, repository or page"
	class="sm:max-w-xl"
>
	<Command.Input placeholder="Search packages, registries, repositories…" bind:value={query} />
	<Command.List class="max-h-[min(60vh,28rem)]">
		{#if nothing}
			<div class="flex flex-col items-center gap-2 py-10 text-sm text-muted-foreground">
				<SearchIcon class="size-5" />
				No results for “{query.trim()}”
			</div>
		{/if}
		{#if packages.length > 0 || searching}
			<Command.Group heading="Packages">
				{#each packages as pkg (`${pkg.kind}/${pkg.flatpakId}`)}
					<Command.LinkItem href={packageHref(pkg.kind, pkg.flatpakId)} value="pkg:{pkg.kind}/{pkg.flatpakId}">
						<PackageIcon iconId={pkg.iconImageId} alt="" class="size-7 rounded-md" />
						<div class="flex min-w-0 flex-1 flex-col">
							<span class="truncate font-medium">{packageTitle(pkg)}</span>
							<span class="truncate font-mono text-xs text-muted-foreground">{pkg.flatpakId}</span>
						</div>
						<KindBadge kind={pkg.kind} />
					</Command.LinkItem>
				{/each}
				{#if searching && packages.length === 0}
					<div class="flex items-center gap-2 px-2 py-3 text-sm text-muted-foreground">
						<Spinner />Searching…
					</div>
				{/if}
			</Command.Group>
		{/if}
		{#if foundRegistries.length > 0}
			<Command.Group heading="Registries">
				{#each foundRegistries as r (r.id)}
					<Command.LinkItem href="/registries/{r.id}" value="registry:{r.id}">
						<ServerIcon />
						<span class="truncate">{r.name}</span>
						<span class="ml-auto truncate font-mono text-xs text-muted-foreground">{r.url}</span>
					</Command.LinkItem>
				{/each}
			</Command.Group>
		{/if}
		{#if foundRepositories.length > 0}
			<Command.Group heading="Repositories">
				{#each foundRepositories as r (r.id)}
					<Command.LinkItem href="/repositories/{r.id}" value="repo:{r.id}">
						<LibraryIcon />
						<span class="truncate">{r.title || r.slug}</span>
						<span class="ml-auto font-mono text-xs text-muted-foreground">{r.slug}</span>
					</Command.LinkItem>
				{/each}
			</Command.Group>
		{/if}
		{#if pages.length > 0 || actions.length > 0}
			<Command.Group heading="Go to">
				{#each pages as item (item.href)}
					<Command.LinkItem href={item.href} value="page:{item.href}">
						<item.icon />{item.title}
					</Command.LinkItem>
				{/each}
				{#each actions as a (a.href)}
					<Command.LinkItem href={a.href} value="action:{a.href}">
						<PlusIcon />{a.title}
					</Command.LinkItem>
				{/each}
			</Command.Group>
		{/if}
	</Command.List>
</Command.Dialog>
