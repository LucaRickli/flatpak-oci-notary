<script lang="ts">
	import { page } from '$app/state';
	import * as Empty from '$lib/components/ui/empty';
	import { Button } from '$lib/components/ui/button';
	import CompassIcon from '@lucide/svelte/icons/compass';
</script>

<Empty.Root class="flex-1">
	<Empty.Header>
		<Empty.Media variant="icon"><CompassIcon /></Empty.Media>
		<Empty.Title>{page.status === 404 ? 'Page not found' : `Error ${page.status}`}</Empty.Title>
		<Empty.Description>{page.error?.message ?? 'Something went wrong.'}</Empty.Description>
	</Empty.Header>
	<Empty.Content>
		<Button href="/">Back to dashboard</Button>
	</Empty.Content>
</Empty.Root>
