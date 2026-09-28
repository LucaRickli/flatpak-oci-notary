import { untrack } from 'svelte';

/**
 * Loads async data into reactive state. Must be created during component
 * initialisation: the fetcher runs inside an effect, so any reactive value it
 * reads synchronously (before its first `await`) triggers a reload when it changes.
 * Previous data is kept while reloading, so pages don't flash back to skeletons.
 * Data is stored as raw (non-proxied) state: replace `current`, don't mutate it.
 */
export class Resource<T> {
	current = $state.raw<T | undefined>(undefined);
	error = $state.raw<unknown>(undefined);
	loading = $state(true);

	#seq = 0;
	#fetcher: () => Promise<T>;

	constructor(fetcher: () => Promise<T>) {
		this.#fetcher = fetcher;
		$effect(() => {
			this.#run();
		});
	}

	/** Reloads the data (e.g. after a mutation or while polling). */
	refresh = (): Promise<void> => untrack(() => this.#run());

	async #run() {
		const seq = ++this.#seq;
		this.loading = true;
		try {
			const value = await this.#fetcher();
			if (seq === this.#seq) {
				this.current = value;
				this.error = undefined;
			}
		} catch (err) {
			if (seq === this.#seq) this.error = err;
		} finally {
			if (seq === this.#seq) this.loading = false;
		}
	}
}
