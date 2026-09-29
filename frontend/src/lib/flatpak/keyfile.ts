/**
 * A GLib key file ("desktop entry" / .ini style) parser that follows the rules
 * of GLib's gkeyfile.c, which flatpak uses to read its metadata:
 *
 * - Lines end at '\n'; a '\r' right before it is dropped (CRLF files).
 * - Leading ASCII whitespace is skipped on every line. Empty lines and lines
 *   starting with '#' are comments.
 * - `[Group Name]` starts a group; spaces/tabs after the ']' are tolerated. A
 *   group that appears twice is merged into the first one.
 * - `key = value`: whitespace before '=' is trimmed from the key, whitespace
 *   after '=' is dropped from the value, trailing whitespace is kept.
 * - `key[locale]=value` are translations. They are kept separately and never
 *   shadow the untranslated key.
 * - A key that appears twice in a group: the last value wins.
 * - Anything else (a key before the first group, a line without '=', an
 *   invalid group or key name) is an error. GLib rejects the whole file on the
 *   first error, so flatpak would too; this parser records every error and
 *   carries on so callers can still show something.
 *
 * Values are stored raw; `string()` / `stringList()` apply GLib's escapes
 * (\s \n \t \r \\ and, in lists, \;).
 */

export interface KeyFileError {
	/** 1-based line number. */
	line: number;
	message: string;
}

interface Group {
	/** Untranslated keys, in order of first appearance. */
	values: Map<string, string>;
	/** key -> locale -> raw value. */
	translations: Map<string, Map<string, string>>;
}

// g_ascii_isspace: space, \t, \n, \v, \f, \r.
const ASCII_SPACE = /[ \t\n\v\f\r]/;
const isAsciiSpace = (c: string | undefined) => c !== undefined && ASCII_SPACE.test(c);
// eslint-disable-next-line no-control-regex
const CONTROL_CHAR = /[\u0000-\u001f\u007f]/;

export class KeyFile {
	readonly #groups = new Map<string, Group>();
	/** Parse errors; empty when GLib would accept the file. */
	readonly errors: KeyFileError[] = [];

	/** True when GLib would load this file without error. */
	get valid(): boolean {
		return this.errors.length === 0;
	}

	/** Group names in order of first appearance. */
	groups(): string[] {
		return [...this.#groups.keys()];
	}

	hasGroup(group: string): boolean {
		return this.#groups.has(group);
	}

	/** Untranslated keys of a group, in order of first appearance. */
	keys(group: string): string[] {
		return [...(this.#groups.get(group)?.values.keys() ?? [])];
	}

	hasKey(group: string, key: string): boolean {
		return this.#groups.get(group)?.values.has(key) ?? false;
	}

	/** The raw (still escaped) value, like g_key_file_get_value. */
	raw(group: string, key: string): string | undefined {
		return this.#groups.get(group)?.values.get(key);
	}

	/** Raw translated values of a key (locale -> value), e.g. name[de]. */
	translations(group: string, key: string): Map<string, string> {
		return new Map(this.#groups.get(group)?.translations.get(key) ?? []);
	}

	/** The unescaped value, like g_key_file_get_string. */
	string(group: string, key: string): string | undefined {
		const raw = this.raw(group, key);
		return raw === undefined ? undefined : unescapeValue(raw);
	}

	/** A ';'-separated list, like g_key_file_get_string_list. */
	stringList(group: string, key: string): string[] | undefined {
		const raw = this.raw(group, key);
		return raw === undefined ? undefined : splitList(raw);
	}

	/**
	 * A boolean like g_key_file_get_boolean: "true"/"1" or "false"/"0"
	 * (case-sensitive, trailing whitespace ignored). Invalid values return
	 * `undefined`; flatpak treats them (and missing keys) as false.
	 */
	boolean(group: string, key: string): boolean | undefined {
		const raw = this.raw(group, key);
		return raw === undefined ? undefined : parseBoolean(raw);
	}

	/**
	 * A base-10 integer like g_key_file_get_integer / get_uint64 (surrounding
	 * whitespace allowed). Invalid values return `undefined`.
	 */
	integer(group: string, key: string): number | undefined {
		const raw = this.raw(group, key);
		return raw === undefined ? undefined : parseInteger(raw);
	}

	#ensureGroup(name: string): Group {
		let group = this.#groups.get(name);
		if (!group) {
			group = { values: new Map(), translations: new Map() };
			this.#groups.set(name, group);
		}
		return group;
	}

	/** Parses GLib key file text. Never throws; see `errors`. */
	static parse(text: string): KeyFile {
		const file = new KeyFile();
		let current: Group | undefined;
		let first: Group | undefined;

		const lines = text.split('\n');
		lines.forEach((rawLine, index) => {
			const lineNo = index + 1;
			const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine;
			let start = 0;
			while (isAsciiSpace(line[start])) start++;
			const content = line.slice(start);
			const fail = (message: string) => file.errors.push({ line: lineNo, message });

			if (content === '' || content.startsWith('#')) return;

			if (isGroupLine(content)) {
				const name = content.slice(1, content.indexOf(']'));
				if (!isGroupName(name)) return fail(`Invalid group name: ${name}`);
				current = file.#ensureGroup(name);
				first ??= current;
				return;
			}

			const eq = content.indexOf('=');
			if (eq <= 0) {
				return fail(`Line “${content}” is not a key-value pair, group, or comment`);
			}
			if (!current) return fail('Key file does not start with a group');

			const key = trimAsciiEnd(content.slice(0, eq));
			let valueStart = eq + 1;
			while (isAsciiSpace(content[valueStart])) valueStart++;
			const value = content.slice(valueStart);

			const parsed = parseKeyName(key);
			if (!parsed) return fail(`Invalid key name: ${key}`);

			if (current === first && key === 'Encoding' && value.toUpperCase() !== 'UTF-8') {
				return fail(`Unsupported encoding “${value}”`);
			}

			if (parsed.locale === undefined) {
				current.values.set(parsed.key, value);
			} else {
				let byLocale = current.translations.get(parsed.key);
				if (!byLocale) current.translations.set(parsed.key, (byLocale = new Map()));
				byLocale.set(parsed.locale, value);
			}
		});

		return file;
	}
}

/** Parses GLib key file text. Never throws; see `KeyFile.errors`. */
export function parseKeyFile(text: string): KeyFile {
	return KeyFile.parse(text);
}

/** `[...]` followed only by spaces/tabs (g_key_file_line_is_group). */
function isGroupLine(line: string): boolean {
	if (line[0] !== '[') return false;
	const close = line.indexOf(']');
	return close > 0 && /^[ \t]*$/.test(line.slice(close + 1));
}

/** Non-empty, no '[', ']' or control characters (g_key_file_is_group_name). */
function isGroupName(name: string): boolean {
	return name !== '' && !/[[\]]/.test(name) && !CONTROL_CHAR.test(name);
}

/**
 * Splits `key` or `key[locale]` (g_key_file_is_key_name + key_get_locale).
 * Keys must be non-empty, must not start or end with a space and must not
 * contain '=', '[' or ']' except for a trailing locale suffix made of
 * alphanumerics and "-_.@".
 */
function parseKeyName(name: string): { key: string; locale?: string } | undefined {
	const m = /^([^=[\]]+)(?:\[([\p{L}\p{N}\-_.@]*)\])?$/u.exec(name);
	if (!m) return undefined;
	const key = m[1];
	if (key.startsWith(' ') || key.endsWith(' ')) return undefined;
	if (m[2] === undefined) return { key };
	// GLib accepts "key[]" but does not treat an empty suffix as a locale.
	if (m[2] === '') return { key: name };
	return { key, locale: m[2] };
}

/**
 * Applies GLib's string escapes: \s \n \t \r \\. Unknown escapes are kept
 * literally (GLib reports them as invalid but keeps the text), a trailing
 * backslash is dropped.
 */
export function unescapeValue(raw: string): string {
	return unescape(raw, false)[0];
}

/**
 * Splits a list value on unescaped ';' like g_key_file_get_string_list: "\;"
 * is a literal ';', a trailing separator does not add an empty element, but
 * empty elements in the middle ("a;;b") are kept.
 */
export function splitList(raw: string): string[] {
	return unescape(raw, true);
}

function unescape(raw: string, list: boolean): string[] {
	const pieces: string[] = [];
	let current = '';
	for (let i = 0; i < raw.length; i++) {
		const c = raw[i];
		if (c === '\\') {
			const next = raw[++i];
			switch (next) {
				case undefined:
					break; // escape at end of line: dropped
				case 's':
					current += ' ';
					break;
				case 'n':
					current += '\n';
					break;
				case 't':
					current += '\t';
					break;
				case 'r':
					current += '\r';
					break;
				case '\\':
					current += '\\';
					break;
				default:
					current += list && next === ';' ? ';' : `\\${next}`;
			}
		} else if (list && c === ';') {
			pieces.push(current);
			current = '';
		} else {
			current += c;
		}
	}
	if (!list || current !== '') pieces.push(current);
	return pieces;
}

/**
 * `s` without trailing ASCII whitespace (GLib's g_ascii_isspace set, not Unicode whitespace).
 * A loop, not /\s+$/: that regex is quadratic on long whitespace runs, and metadata comes from
 * untrusted images.
 */
function trimAsciiEnd(s: string): string {
	let end = s.length;
	while (end > 0 && isAsciiSpace(s[end - 1])) end--;
	return s.slice(0, end);
}

function parseBoolean(raw: string): boolean | undefined {
	const value = trimAsciiEnd(raw);
	if (value === 'true' || value === '1') return true;
	if (value === 'false' || value === '0') return false;
	return undefined;
}

function parseInteger(raw: string): number | undefined {
	const value = raw.trim();
	if (!/^[+-]?\d+$/.test(value)) return undefined;
	const n = Number(value);
	return Number.isSafeInteger(n) ? n : undefined;
}
