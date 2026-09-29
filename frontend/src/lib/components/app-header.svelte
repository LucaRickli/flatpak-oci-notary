<script lang="ts">
	import { page } from '$app/state';
	import * as Sidebar from '$lib/components/ui/sidebar';
	import * as Breadcrumb from '$lib/components/ui/breadcrumb';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { Separator } from '$lib/components/ui/separator';
	import { Badge } from '$lib/components/ui/badge';
	import ModeToggle from './mode-toggle.svelte';
	import { session } from '$lib/session.svelte';
	import { crumb } from '$lib/breadcrumb.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Kbd } from '$lib/components/ui/kbd';
	import { palette } from '$lib/palette.svelte';
	import ShieldOffIcon from '@lucide/svelte/icons/shield-off';
	import SearchIcon from '@lucide/svelte/icons/search';

	const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);

	const sections: Record<string, { label: string; href: string }> = {
		registries: { label: 'Registries', href: '/registries' },
		packages: { label: 'Packages', href: '/packages' },
		// /images/<id> resolves to a package page.
		images: { label: 'Packages', href: '/packages' },
		repositories: { label: 'Repositories', href: '/repositories' }
	};

	const crumbs = $derived.by(() => {
		const [section, sub] = page.url.pathname.split('/').filter(Boolean);
		const items: { label: string; href?: string }[] = [];
		if (!section) return [{ label: 'Overview' }];
		const s = sections[section] ?? { label: section, href: `/${section}` };
		items.push({ label: s.label, href: sub ? s.href : undefined });
		if (sub) {
			const label = sub === 'new' ? 'New' : crumb.path === page.url.pathname ? crumb.label : '…';
			items.push({ label });
		}
		return items;
	});
</script>

<header
	class="sticky top-0 z-10 flex h-14 shrink-0 items-center gap-2 border-b bg-background/90 px-3 backdrop-blur md:rounded-t-xl md:px-4"
>
	<Sidebar.Trigger class="-ml-1" />
	<Separator orientation="vertical" class="mr-2 data-vertical:h-4 data-vertical:self-auto" />
	<Breadcrumb.Root class="min-w-0 flex-1">
		<Breadcrumb.List class="flex-nowrap">
			{#each crumbs as item, i (i)}
				{#if i > 0}<Breadcrumb.Separator />{/if}
				<Breadcrumb.Item class="min-w-0">
					{#if item.href}
						<Breadcrumb.Link href={item.href}>{item.label}</Breadcrumb.Link>
					{:else}
						<Breadcrumb.Page class="truncate">{item.label}</Breadcrumb.Page>
					{/if}
				</Breadcrumb.Item>
			{/each}
		</Breadcrumb.List>
	</Breadcrumb.Root>
	<Button
		variant="outline"
		class="hidden w-56 justify-start text-muted-foreground md:inline-flex lg:w-72"
		onclick={() => (palette.open = true)}
	>
		<SearchIcon data-icon="inline-start" />
		<span class="flex-1 text-left">Search…</span>
		<Kbd>{isMac ? '⌘' : 'Ctrl'} K</Kbd>
	</Button>
	<Button variant="ghost" size="icon-sm" class="md:hidden" aria-label="Search" onclick={() => (palette.open = true)}>
		<SearchIcon />
	</Button>
	{#if session.me && !session.me.authEnabled}
		<Tooltip.Root>
			<Tooltip.Trigger>
				{#snippet child({ props })}
					<Badge variant="warning" {...props} aria-label="Authentication disabled">
						<ShieldOffIcon data-icon="inline-start" />
						<span class="hidden lg:inline">No auth</span>
					</Badge>
				{/snippet}
			</Tooltip.Trigger>
			<Tooltip.Content class="max-w-xs">
				Anyone who can reach this server can administer it. Configure OIDC to require sign-in.
			</Tooltip.Content>
		</Tooltip.Root>
	{/if}
	<ModeToggle />
</header>
