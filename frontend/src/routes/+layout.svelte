<script lang="ts">
	import './layout.css';
	import { onMount } from 'svelte';
	import { ModeWatcher } from 'mode-watcher';
	import { Toaster } from '$lib/components/ui/sonner';
	import * as Sidebar from '$lib/components/ui/sidebar';
	import * as Empty from '$lib/components/ui/empty';
	import { Button } from '$lib/components/ui/button';
	import { Spinner } from '$lib/components/ui/spinner';
	import AppSidebar from '$lib/components/app-sidebar.svelte';
	import AppHeader from '$lib/components/app-header.svelte';
	import SignIn from '$lib/components/sign-in.svelte';
	import CommandPalette from '$lib/components/command-palette.svelte';
	import { errorMessage, onUnauthenticated, systemClient } from '$lib/api';
	import { session } from '$lib/session.svelte';
	import ServerCrashIcon from '@lucide/svelte/icons/server-crash';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';

	let { children } = $props();

	let status = $state<'loading' | 'ready' | 'error'>('loading');
	let loadError = $state<unknown>();

	async function init() {
		status = 'loading';
		try {
			const [me, info] = await Promise.all([
				systemClient.getMe({}),
				systemClient.getInfo({}).catch(() => undefined)
			]);
			session.me = me;
			session.info = info;
			session.expired = false;
			status = 'ready';
		} catch (err) {
			loadError = err;
			status = 'error';
		}
	}

	onMount(() => {
		init();
		return onUnauthenticated(() => (session.expired = true));
	});

	const signedOut = $derived(
		session.me !== undefined &&
			(session.expired || (session.me.authEnabled && !session.me.authenticated))
	);
</script>

<svelte:head>
	<title>Flatpak OCI Notary</title>
</svelte:head>

<ModeWatcher />
<Toaster richColors closeButton />

{#if status === 'loading'}
	<div class="flex min-h-svh items-center justify-center">
		<Spinner class="size-6 text-muted-foreground" />
	</div>
{:else if status === 'error'}
	<div class="flex min-h-svh items-center justify-center p-4">
		<Empty.Root>
			<Empty.Header>
				<Empty.Media variant="icon"><ServerCrashIcon /></Empty.Media>
				<Empty.Title>Can't reach the server</Empty.Title>
				<Empty.Description>{errorMessage(loadError)}</Empty.Description>
			</Empty.Header>
			<Empty.Content>
				<Button variant="outline" onclick={init}><RefreshCwIcon data-icon="inline-start" />Retry</Button>
			</Empty.Content>
		</Empty.Root>
	</div>
{:else if signedOut}
	<SignIn expired={session.expired} />
{:else}
	<Sidebar.Provider>
		<AppSidebar />
		<Sidebar.Inset class="min-w-0">
			<AppHeader />
			<div class="mx-auto flex w-full max-w-7xl flex-1 flex-col gap-6 px-4 py-5 md:px-8 md:py-7">
				{@render children()}
			</div>
		</Sidebar.Inset>
	</Sidebar.Provider>
	<CommandPalette />
{/if}
