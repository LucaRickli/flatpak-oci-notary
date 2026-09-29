import { parseKeyFile, splitList, unescapeValue } from './keyfile.ts';
import { assert, assertEquals, test } from './test-helpers.ts';

test('groups, keys, comments and blank lines', () => {
	const f = parseKeyFile(
		[
			'# leading comment',
			'',
			'[Application]',
			'name=org.example.App',
			'   # indented comment',
			'\t',
			'  command = example  ',
			'[Context]',
			'shared=network;'
		].join('\n')
	);
	assertEquals(f.errors, []);
	assertEquals(f.groups(), ['Application', 'Context']);
	assertEquals(f.keys('Application'), ['name', 'command']);
	assertEquals(f.string('Application', 'name'), 'org.example.App');
	// Whitespace around '=' is dropped, trailing whitespace of the value is kept.
	assertEquals(f.raw('Application', 'command'), 'example  ');
	assertEquals(f.stringList('Context', 'shared'), ['network']);
	assertEquals(f.string('Context', 'missing'), undefined);
	assertEquals(f.string('Missing', 'name'), undefined);
});

test('group headers tolerate trailing blanks but nothing else', () => {
	const ok = parseKeyFile('  [Runtime] \t\nname=x');
	assertEquals(ok.errors, []);
	assertEquals(ok.string('Runtime', 'name'), 'x');
	assertEquals(ok.hasGroup('Runtime'), true);

	const bad = parseKeyFile('[Runtime] trailing\nname=x');
	assertEquals(bad.valid, false);
	assertEquals(bad.errors[0].line, 1);
});

test('group names with spaces are fine, brackets inside are not', () => {
	const f = parseKeyFile('[Extension org.example.App.Locale]\ndirectory=share/runtime/locale\n[Session Bus Policy]\norg.x=talk');
	assertEquals(f.groups(), ['Extension org.example.App.Locale', 'Session Bus Policy']);
	assertEquals(parseKeyFile('[a[b]\n').valid, false);
	assertEquals(parseKeyFile('[]\n').valid, false);
});

test('escape sequences', () => {
	const f = parseKeyFile('[G]\nk=\\sLead\\tTab\\nNL\\rCR\\\\Back');
	assertEquals(f.string('G', 'k'), ' Lead\tTab\nNL\rCR\\Back');
	// Unknown escapes stay as written; a trailing backslash is dropped.
	assertEquals(unescapeValue('a\\qb'), 'a\\qb');
	assertEquals(unescapeValue('end\\'), 'end');
	// \; is only special in lists.
	assertEquals(unescapeValue('a\\;b'), 'a\\;b');
	// Leading whitespace is stripped from values, so \s is needed to keep it.
	assertEquals(parseKeyFile('[G]\nk=   x').string('G', 'k'), 'x');
});

test('lists', () => {
	assertEquals(splitList('a;b;c;'), ['a', 'b', 'c']);
	assertEquals(splitList('a;b;c'), ['a', 'b', 'c']);
	assertEquals(splitList('a;;b'), ['a', '', 'b']);
	assertEquals(splitList(';a'), ['', 'a']);
	assertEquals(splitList(';'), ['']);
	assertEquals(splitList(''), []);
	assertEquals(splitList('a\\;b;c'), ['a;b', 'c']);
	assertEquals(splitList('back\\\\;x'), ['back\\', 'x']);
	assertEquals(splitList('sp\\sace;tab\\t'), ['sp ace', 'tab\t']);
});

test('translations are kept separately and never shadow the key', () => {
	const f = parseKeyFile('[G]\nname[de]=Rechner\nname=Calculator\nname[sr@latin]=Kalkulator\nempty[]=x');
	assertEquals(f.keys('G'), ['name', 'empty[]']);
	assertEquals(f.string('G', 'name'), 'Calculator');
	assertEquals(
		f.translations('G', 'name'),
		new Map([
			['de', 'Rechner'],
			['sr@latin', 'Kalkulator']
		])
	);
	assertEquals(parseKeyFile('[G]\nname [de]=x').valid, false);
	assertEquals(parseKeyFile('[G]\nname[de]x=y').valid, false);
});

test('duplicate keys: last wins; duplicate groups merge', () => {
	const f = parseKeyFile('[A]\nk=1\nj=x\n[B]\nk=b\n[A]\nk=2');
	assertEquals(f.groups(), ['A', 'B']);
	assertEquals(f.keys('A'), ['k', 'j']);
	assertEquals(f.string('A', 'k'), '2');
	assertEquals(f.string('B', 'k'), 'b');
});

test('CRLF line endings', () => {
	const f = parseKeyFile('[G]\r\nk=v\r\nlist=a;b;\r\n');
	assertEquals(f.errors, []);
	assertEquals(f.raw('G', 'k'), 'v');
	assertEquals(f.stringList('G', 'list'), ['a', 'b']);
});

test('errors carry line numbers and parsing continues', () => {
	const f = parseKeyFile(['k=before group', '[G]', 'no equals sign', '=novalue', 'bad]key=1', 'ok=1'].join('\n'));
	assertEquals(
		f.errors.map((e) => e.line),
		[1, 3, 4, 5]
	);
	assertEquals(f.string('G', 'ok'), '1');
	assert(f.errors[0].message.includes('does not start with a group'));
});

test('Encoding must be UTF-8 in the first group', () => {
	assertEquals(parseKeyFile('[G]\nEncoding=UTF-8').valid, true);
	assertEquals(parseKeyFile('[G]\nEncoding=utf-8').valid, true);
	assertEquals(parseKeyFile('[G]\nEncoding=ISO-8859-1').valid, false);
	assertEquals(parseKeyFile('[G]\n[H]\nEncoding=ISO-8859-1').valid, true);
});

test('booleans and integers', () => {
	const f = parseKeyFile('[G]\na=true\nb=1\nc=false\nd=0\ne=True\nf=true  \ng=yes\ni= 42 \nj=4x\nk=-7');
	assertEquals(
		['a', 'b', 'c', 'd', 'e', 'f', 'g'].map((k) => f.boolean('G', k)),
		[true, true, false, false, undefined, true, undefined]
	);
	assertEquals(f.boolean('G', 'missing'), undefined);
	assertEquals(f.integer('G', 'i'), 42);
	assertEquals(f.integer('G', 'j'), undefined);
	assertEquals(f.integer('G', 'k'), -7);
});

test('long whitespace runs parse in linear time', () => {
	const spaces = ' '.repeat(200_000);
	const start = Date.now();
	const f = parseKeyFile(`[Application]\nname=x\nkey${spaces}=v\nx${spaces}y=1\nb=true${spaces}`);
	const ms = Date.now() - start;
	assertEquals(f.string('Application', 'key'), 'v');
	assertEquals(f.boolean('Application', 'b'), true);
	assertEquals(f.string('Application', `x${spaces}y`), '1'); // inner whitespace is part of the key
	assertEquals(f.errors, []);
	// Generous bound: the quadratic trim took minutes for this input.
	assert(ms < 2000, `took ${ms} ms`);
});

test('empty input is a valid, empty key file', () => {
	const f = parseKeyFile('');
	assertEquals(f.valid, true);
	assertEquals(f.groups(), []);
});
