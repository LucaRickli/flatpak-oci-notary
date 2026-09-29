<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import { Button } from '$lib/components/ui/button';
	import type { MissingRuntime } from '$lib/api';
	import { refHref } from '$lib/links';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

	let {
		missing,
		registryName
	}: {
		missing: MissingRuntime[];
		/** Registry of the repository, for the hint. */
		registryName?: string;
	} = $props();

	const COLLAPSED = 3;
	const MAX_NEEDED_BY = 3;
	let expanded = $state(false);
	const visible = $derived(expanded ? missing : missing.slice(0, COLLAPSED));
</script>

<Alert.Root class="border-amber-500/40 *:[svg]:text-amber-600 dark:*:[svg]:text-amber-500">
	<TriangleAlertIcon />
	<Alert.Title>
		{missing.length}
		{missing.length === 1 ? 'runtime is' : 'runtimes are'} needed but not served by this repository
	</Alert.Title>
	<Alert.Description class="flex flex-col gap-3">
		<div>
			Installing the flatpaks below fails unless the client has another remote providing the runtime (apps need their
			runtime, and so do runtimes and extensions that download extra data). Include it in the rules if
			{registryName || 'the registry'} has it, publish a repository for a registry that has it, or tell users to add a
			remote that provides it.
		</div>
		<ul class="flex w-full flex-col divide-y rounded-md border">
			{#each visible as item (item.runtime)}
				{@const href = refHref(item.runtime)}
				<li class="flex min-w-0 flex-col gap-1.5 p-3">
					{#if href}
						<a {href} class="font-mono text-xs font-medium break-all text-foreground hover:underline">{item.runtime}</a>
					{:else}
						<span class="font-mono text-xs font-medium break-all text-foreground">{item.runtime}</span>
					{/if}
					<span class="text-xs break-words">
						Needed by
						<span class="font-mono">{item.neededBy.slice(0, MAX_NEEDED_BY).join(', ')}</span>
						{#if item.neededBy.length > MAX_NEEDED_BY}
							<span title={item.neededBy.slice(MAX_NEEDED_BY).join(', ')}>
								and {item.neededBy.length - MAX_NEEDED_BY} more
							</span>
						{/if}
					</span>
					{#if item.availableIn.length > 0}
						<div class="flex flex-wrap gap-x-3 gap-y-1 text-xs">
							<span>Available in</span>
							{#each item.availableIn as registry (registry.id)}
								<span>
									<a href="/registries/{registry.id}" class="font-medium">{registry.name}</a>
									(<a href="/repositories/new?registry={encodeURIComponent(registry.id)}">publish a repository</a>)
								</span>
							{/each}
						</div>
					{:else}
						<span class="text-xs">Not indexed in any other registry: clients need a remote that provides it.</span>
					{/if}
				</li>
			{/each}
		</ul>
		{#if missing.length > COLLAPSED}
			<div>
				<Button variant="outline" size="sm" onclick={() => (expanded = !expanded)}>
					{expanded ? 'Show fewer' : `Show all ${missing.length}`}
				</Button>
			</div>
		{/if}
	</Alert.Description>
</Alert.Root>
