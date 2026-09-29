// Minimal test helpers for `deno test`. Deno is declared locally (module
// scope) so svelte-check, which only knows the DOM lib, type-checks the tests
// too; no jsr:/node: imports for the same reason.

declare const Deno: { test(name: string, fn: () => void | Promise<void>): void };

export function test(name: string, fn: () => void | Promise<void>): void {
	Deno.test(name, fn);
}

/** Canonical JSON-like form: sorted object keys, undefined properties dropped, Maps as entry lists. */
function canon(value: unknown): unknown {
	if (value instanceof Map) return { $map: [...value].map(([k, v]) => [canon(k), canon(v)]) };
	if (Array.isArray(value)) return value.map(canon);
	if (typeof value === 'bigint') return { $bigint: value.toString() };
	if (value && typeof value === 'object') {
		return Object.fromEntries(
			Object.entries(value)
				.filter(([, v]) => v !== undefined)
				.sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
				.map(([k, v]) => [k, canon(v)])
		);
	}
	return value;
}

export function assertEquals(actual: unknown, expected: unknown, message = ''): void {
	const a = JSON.stringify(canon(actual), null, 2);
	const e = JSON.stringify(canon(expected), null, 2);
	if (a !== e) {
		throw new Error(`${message ? `${message}\n` : ''}Expected:\n${e}\nActual:\n${a}`);
	}
}

export function assert(condition: unknown, message = 'Assertion failed'): asserts condition {
	if (!condition) throw new Error(message);
}
