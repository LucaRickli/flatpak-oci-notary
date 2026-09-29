<script lang="ts">
	import * as Table from '$lib/components/ui/table';
	import * as Select from '$lib/components/ui/select';
	import * as Alert from '$lib/components/ui/alert';
	import * as Empty from '$lib/components/ui/empty';
	import { Input } from '$lib/components/ui/input';
	import { Button } from '$lib/components/ui/button';
	import PackagePicker from './package-picker.svelte';
	import { includeEverything, matchesEverything, type SourceDraft } from '$lib/sources';
	import AsteriskIcon from '@lucide/svelte/icons/asterisk';
	import PackagePlusIcon from '@lucide/svelte/icons/package-plus';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import XIcon from '@lucide/svelte/icons/x';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import ListFilterIcon from '@lucide/svelte/icons/list-filter';
	import { tick } from 'svelte';

	let {
		sources = $bindable([]),
		registryId
	}: {
		sources: SourceDraft[];
		/** Registry the repository is bound to (empty = none chosen yet). */
		registryId: string;
	} = $props();

	let pickerOpen = $state(false);
	let table = $state<HTMLElement | null>(null);

	const hasEverything = $derived(sources.some((s) => !s.exclude && matchesEverything(s)));
	const hasInclude = $derived(sources.some((s) => !s.exclude));

	function add(rule: SourceDraft) {
		sources = [...sources, rule];
	}

	async function addCustom() {
		add(includeEverything());
		await tick();
		table?.querySelector<HTMLInputElement>('tbody tr:last-child input')?.focus();
	}

	function remove(index: number) {
		sources = sources.filter((_, i) => i !== index);
	}
</script>

<div class="flex flex-col gap-4">
	<div class="flex flex-wrap gap-2">
		<Button type="button" variant="outline" size="sm" disabled={hasEverything} onclick={() => add(includeEverything())}>
			<AsteriskIcon data-icon="inline-start" />Include everything
		</Button>
		<Button type="button" variant="outline" size="sm" disabled={!registryId} onclick={() => (pickerOpen = true)}>
			<PackagePlusIcon data-icon="inline-start" />Add package
		</Button>
		<Button type="button" variant="outline" size="sm" onclick={addCustom}>
			<PlusIcon data-icon="inline-start" />Custom rule
		</Button>
	</div>

	{#if sources.length === 0}
		<Empty.Root class="border border-dashed p-6 md:p-6">
			<Empty.Header>
				<Empty.Media variant="icon"><ListFilterIcon /></Empty.Media>
				<Empty.Title>No rules</Empty.Title>
				<Empty.Description>
					Without rules the repository serves nothing. Include everything, pick packages, or write a custom rule.
				</Empty.Description>
			</Empty.Header>
		</Empty.Root>
	{:else}
		<div class="rounded-lg border" bind:this={table}>
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head class="w-32 pl-3">Rule</Table.Head>
						<Table.Head class="min-w-48">Repository pattern</Table.Head>
						<Table.Head class="min-w-40">Ref pattern</Table.Head>
						<Table.Head class="min-w-28">Tag pattern</Table.Head>
						<Table.Head class="w-10 pr-3"><span class="sr-only">Remove</span></Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each sources as rule, i (i)}
						<Table.Row>
							<Table.Cell class="pl-3">
								<Select.Root
									type="single"
									value={rule.exclude ? 'exclude' : 'include'}
									onValueChange={(v) => (rule.exclude = v === 'exclude')}
								>
									<Select.Trigger size="sm" class="w-full" aria-label="Rule type">
										{rule.exclude ? 'Exclude' : 'Include'}
									</Select.Trigger>
									<Select.Content>
										<Select.Group>
											<Select.Item value="include">Include</Select.Item>
											<Select.Item value="exclude">Exclude</Select.Item>
										</Select.Group>
									</Select.Content>
								</Select.Root>
							</Table.Cell>
							<Table.Cell>
								<Input bind:value={rule.repositoryPattern} placeholder="*" class="h-7 font-mono text-xs" aria-label="Repository pattern" />
							</Table.Cell>
							<Table.Cell>
								<Input bind:value={rule.refPattern} placeholder="*" class="h-7 font-mono text-xs" aria-label="Ref pattern" />
							</Table.Cell>
							<Table.Cell>
								<Input bind:value={rule.tagPattern} placeholder="*" class="h-7 font-mono text-xs" aria-label="Tag pattern" />
							</Table.Cell>
							<Table.Cell class="pr-3">
								<Button type="button" variant="ghost" size="icon-sm" aria-label="Remove rule" onclick={() => remove(i)}>
									<XIcon />
								</Button>
							</Table.Cell>
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</div>
		{#if !hasInclude}
			<Alert.Root>
				<TriangleAlertIcon />
				<Alert.Title>Only exclude rules</Alert.Title>
				<Alert.Description>An image must match at least one include rule, so this repository serves nothing.</Alert.Description>
			</Alert.Root>
		{/if}
	{/if}

	<p class="text-xs text-muted-foreground">
		An image is served if it matches at least one include rule and no exclude rule — excludes always win. Patterns are
		globs: <code class="font-mono">*</code> matches any characters (including <code class="font-mono">/</code>),
		<code class="font-mono">?</code> a single one, and empty matches everything. E.g. repository
		<code class="font-mono">myorg/*</code>, ref <code class="font-mono">app/org.gnome.*</code>, tag
		<code class="font-mono">stable-*</code>.
	</p>
</div>

{#if registryId}
	<PackagePicker bind:open={pickerOpen} {registryId} {sources} onpick={add} />
{/if}
