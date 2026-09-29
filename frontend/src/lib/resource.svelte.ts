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
			this.#run(false);
		});
	}

	/** Reloads the data (e.g. after a mutation). */
	refresh = (): Promise<void> => untrack(() => this.#run(false));

	/**
	 * Reloads in the background (periodic refresh): `loading` stays unchanged, and
	 * a failure keeps the data already shown instead of replacing it with the error.
	 * Superseded by any newer load, like `refresh`.
	 */
	poll = (): Promise<void> => untrack(() => this.#run(true));

	async #run(quiet: boolean) {
		const seq = ++this.#seq;
		if (!quiet) this.loading = true;
		try {
			const value = await this.#fetcher();
			if (seq === this.#seq) {
				this.current = value;
				this.error = undefined;
			}
		} catch (err) {
			if (seq === this.#seq && !(quiet && this.current !== undefined)) this.error = err;
		} finally {
			if (seq === this.#seq) this.loading = false;
		}
	}
}
