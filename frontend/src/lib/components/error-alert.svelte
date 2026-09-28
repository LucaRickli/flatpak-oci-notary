<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import { Button } from '$lib/components/ui/button';
	import { errorMessage } from '$lib/api';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';

	let {
		error,
		title = 'Could not load data',
		onretry
	}: { error: unknown; title?: string; onretry?: () => void } = $props();
</script>

<Alert.Root variant="destructive">
	<CircleAlertIcon />
	<Alert.Title>{title}</Alert.Title>
	<Alert.Description>{errorMessage(error)}</Alert.Description>
	{#if onretry}
		<Alert.Action>
			<Button variant="outline" size="sm" onclick={onretry}>
				<RefreshCwIcon data-icon="inline-start" />Retry
			</Button>
		</Alert.Action>
	{/if}
</Alert.Root>
