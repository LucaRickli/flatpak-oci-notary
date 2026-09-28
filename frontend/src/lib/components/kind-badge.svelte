<script lang="ts">
	import { Badge } from '$lib/components/ui/badge';
	import { RefKind } from '$lib/api';
	import { kindLabel } from '$lib/format';

	let { kind }: { kind: RefKind } = $props();
</script>

<Badge variant={kind === RefKind.APP ? 'default' : kind === RefKind.RUNTIME ? 'secondary' : 'outline'}>
	{kindLabel(kind)}
</Badge>
