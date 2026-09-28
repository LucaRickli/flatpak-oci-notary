<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { urls } from '$lib/api';
	import StampIcon from '@lucide/svelte/icons/stamp';
	import LogInIcon from '@lucide/svelte/icons/log-in';

	let { expired = false }: { expired?: boolean } = $props();
</script>

<div class="flex min-h-svh items-center justify-center bg-muted/40 p-4">
	<Card.Root class="w-full max-w-sm">
		<Card.Header class="items-center text-center">
			<div
				class="mx-auto mb-2 flex size-12 items-center justify-center rounded-xl bg-primary text-primary-foreground"
			>
				<StampIcon class="size-6" />
			</div>
			<Card.Title class="text-xl">Flatpak OCI Notary</Card.Title>
			<Card.Description>
				{expired
					? 'Your session has expired. Sign in again to continue.'
					: 'Sign in to manage registries and repositories.'}
			</Card.Description>
		</Card.Header>
		<Card.Footer>
			<Button href={urls.login()} data-sveltekit-reload class="w-full" size="lg">
				<LogInIcon data-icon="inline-start" />
				Sign in
			</Button>
		</Card.Footer>
	</Card.Root>
</div>
