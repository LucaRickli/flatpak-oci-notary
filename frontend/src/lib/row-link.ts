/**
 * Click handler of a table row that links somewhere: navigates with `go` unless the click was on
 * a link or button inside the row, which handle it themselves. (Links must not stop propagation:
 * SvelteKit's router listens on the document, so a stopped click becomes a full page load.)
 */
export function rowClick(go: () => unknown): (event: MouseEvent) => void {
	return (event) => {
		if (event.target instanceof Element && event.target.closest('a, button')) return;
		go();
	};
}
