import type { Component } from 'svelte';
import LayoutDashboardIcon from '@lucide/svelte/icons/layout-dashboard';
import ServerIcon from '@lucide/svelte/icons/server';
import LayoutGridIcon from '@lucide/svelte/icons/layout-grid';
import LibraryIcon from '@lucide/svelte/icons/library';

export interface NavItem {
	title: string;
	href: string;
	icon: Component;
	/** Sidebar group. */
	group: 'browse' | 'manage';
}

/** Main navigation, shared by the sidebar and the command palette. */
export const NAV_ITEMS: NavItem[] = [
	{ title: 'Overview', href: '/', icon: LayoutDashboardIcon, group: 'browse' },
	{ title: 'Packages', href: '/packages', icon: LayoutGridIcon, group: 'browse' },
	{ title: 'Registries', href: '/registries', icon: ServerIcon, group: 'manage' },
	{ title: 'Repositories', href: '/repositories', icon: LibraryIcon, group: 'manage' }
];
