<script lang="ts">
	import { afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import * as Sidebar from '$lib/components/ui/sidebar';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import * as Avatar from '$lib/components/ui/avatar';
	import { systemClient, type GetOverviewResponse } from '$lib/api';
	import { session, reportError } from '$lib/session.svelte';
	import { initials } from '$lib/format';
	import { NAV_ITEMS, type NavItem } from '$lib/nav';
	import StampIcon from '@lucide/svelte/icons/stamp';
	import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
	import LogOutIcon from '@lucide/svelte/icons/log-out';

	const sidebar = Sidebar.useSidebar();

	function isActive(href: string) {
		const path = page.url.pathname;
		if (href === '/') return path === '/';
		// /images/<id> resolves to a package page.
		if (href === '/packages' && path.startsWith('/images/')) return true;
		return path === href || path.startsWith(`${href}/`);
	}

	// Counts next to the nav items; refreshed on every navigation (cheap, and keeps them current
	// after creating or deleting things).
	let overview = $state.raw<GetOverviewResponse>();
	function loadCounts() {
		systemClient.getOverview({}).then(
			(o) => (overview = o),
			() => {}
		);
	}
	// The sidebar mounts after the first navigation, so load once on mount too.
	onMount(loadCounts);
	afterNavigate(({ type }) => {
		if (type !== 'enter') loadCounts();
	});
	function count(href: string): number | undefined {
		if (!overview) return undefined;
		if (href === '/packages') return overview.apps + overview.runtimes;
		if (href === '/registries') return overview.registries;
		if (href === '/repositories') return overview.repositories;
		return undefined;
	}

	const groups: { label: string; items: NavItem[] }[] = [
		{ label: 'Browse', items: NAV_ITEMS.filter((i) => i.group === 'browse') },
		{ label: 'Manage', items: NAV_ITEMS.filter((i) => i.group === 'manage') }
	];

	const user = $derived(session.me?.user);
	const displayName = $derived(user?.name || user?.email || user?.subject || 'Signed in');

	async function signOut() {
		try {
			await systemClient.logout({});
			location.reload();
		} catch (err) {
			reportError(err, 'Could not sign out');
		}
	}

	const semverRegex = /^(\d+\.\d+\.\d+)(?:-.+)?$/;
	const version = $derived(
		session.info?.version
			? `${semverRegex.test(session.info.version) ? 'v' : ''}${session.info.version}`
			: 'Unknown version'
	);
</script>

<Sidebar.Root collapsible="icon" variant="inset">
	<Sidebar.Header>
		<Sidebar.Menu>
			<Sidebar.MenuItem>
				<Sidebar.MenuButton size="lg" tooltipContent="Flatpak OCI Notary">
					{#snippet child({ props })}
						<a href="/" {...props} onclick={() => sidebar.setOpenMobile(false)}>
							<div
								class="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground shadow-sm"
							>
								<StampIcon class="size-4" />
							</div>
							<div class="grid flex-1 text-left text-sm leading-tight">
								<span class="truncate font-semibold">OCI Notary</span>
								<span class="truncate text-xs text-muted-foreground">Flatpak · {version}</span>
							</div>
						</a>
					{/snippet}
				</Sidebar.MenuButton>
			</Sidebar.MenuItem>
		</Sidebar.Menu>
	</Sidebar.Header>

	<Sidebar.Content>
		{#each groups as group (group.label)}
			<Sidebar.Group>
				<Sidebar.GroupLabel>{group.label}</Sidebar.GroupLabel>
				<Sidebar.GroupContent>
					<Sidebar.Menu>
						{#each group.items as item (item.href)}
							{@const n = count(item.href)}
							<Sidebar.MenuItem>
								<Sidebar.MenuButton isActive={isActive(item.href)} tooltipContent={item.title}>
									{#snippet child({ props })}
										<a href={item.href} {...props} onclick={() => sidebar.setOpenMobile(false)}>
											<item.icon />
											<span>{item.title}</span>
										</a>
									{/snippet}
								</Sidebar.MenuButton>
								{#if n !== undefined}
									<Sidebar.MenuBadge class="text-muted-foreground">{n.toLocaleString()}</Sidebar.MenuBadge>
								{/if}
							</Sidebar.MenuItem>
						{/each}
					</Sidebar.Menu>
				</Sidebar.GroupContent>
			</Sidebar.Group>
		{/each}
	</Sidebar.Content>

	{#if session.me?.authEnabled}
		<Sidebar.Footer>
			<Sidebar.Menu>
				<Sidebar.MenuItem>
					<DropdownMenu.Root>
						<DropdownMenu.Trigger>
							{#snippet child({ props })}
								<Sidebar.MenuButton size="lg" {...props}>
									<Avatar.Root class="size-8 rounded-lg">
										<Avatar.Fallback class="rounded-lg">{initials(displayName)}</Avatar.Fallback>
									</Avatar.Root>
									<div class="grid flex-1 text-left text-sm leading-tight">
										<span class="truncate font-medium">{displayName}</span>
										{#if user?.email && user.email !== displayName}
											<span class="truncate text-xs text-muted-foreground">{user.email}</span>
										{/if}
									</div>
									<ChevronsUpDownIcon class="ml-auto" />
								</Sidebar.MenuButton>
							{/snippet}
						</DropdownMenu.Trigger>
						<DropdownMenu.Content
							class="w-(--bits-dropdown-menu-anchor-width) min-w-56"
							side={sidebar.isMobile ? 'bottom' : 'right'}
							align="end"
						>
							<DropdownMenu.Group>
								<DropdownMenu.Label class="font-normal">
									<div class="grid text-sm leading-tight">
										<span class="truncate font-medium">{displayName}</span>
										{#if user?.email}
											<span class="truncate text-xs text-muted-foreground">{user.email}</span>
										{/if}
									</div>
								</DropdownMenu.Label>
							</DropdownMenu.Group>
							<DropdownMenu.Separator />
							<DropdownMenu.Group>
								<DropdownMenu.Item onclick={signOut}>
									<LogOutIcon />
									Sign out
								</DropdownMenu.Item>
							</DropdownMenu.Group>
						</DropdownMenu.Content>
					</DropdownMenu.Root>
				</Sidebar.MenuItem>
			</Sidebar.Menu>
		</Sidebar.Footer>
	{/if}
	<Sidebar.Rail />
</Sidebar.Root>
