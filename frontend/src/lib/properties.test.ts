import test from "node:test";
import assert from "node:assert/strict";
import { parseProperties, type Property } from "./properties.ts";

const one = (key: string, value: string): Property => ({ key, values: [value], list: false });
const list = (key: string, ...values: string[]): Property => ({ key, values, list: true });
const empty = (key: string): Property => ({ key, values: [], list: false });

test("parseProperties reads single values and lists, in file order", () => {
  const cases: [string, Property[]][] = [
    ["", []],
    ["\n\n  \n", []],
    ["tags:\n  - a\n  - b", [list("tags", "a", "b")]],
    ["tags:\n- a\n- b", [list("tags", "a", "b")]], // items written level with the key
    ["tags:\n  - only", [list("tags", "only")]],
    ["source: https://x.y/z", [one("source", "https://x.y/z")]],
    ["tags: [a, \"b c\"]", [list("tags", "a", "b c")]],
    ["tags: [ a ,b,  'c, d' , ]", [list("tags", "a", "b", "c, d")]],
    ["tags: []", [list("tags")]],
    ["tags: [a] # my tags", [list("tags", "a")]],
    ["tags: # my tags\n- a\n- b", [list("tags", "a", "b")]], // a comment after the key leaves the value below it
    ["tags: # my tags\n  - a", [list("tags", "a")]],
    ["created: 2026-10-26T10:15:00\ndone: true\nn: 42", [one("created", "2026-10-26T10:15:00"), one("done", "true"), one("n", "42")]],
    ["date created: 2026-10-26\ntítulo: Hola", [one("date created", "2026-10-26"), one("título", "Hola")]],
    ["title: A\ntags:\n  - x\nsource: S\naliases: [y]", [one("title", "A"), list("tags", "x"), one("source", "S"), list("aliases", "y")]],
    ["tags:\n  - \"a: b\"\n  - 'c # d'\n  -   e   # note\n  -", [list("tags", "a: b", "c # d", "e")]],
    ["---\ntags: a\n---", [one("tags", "a")]], // the fences may come along
  ];
  for (const [yaml, want] of cases) assert.deepEqual(parseProperties(yaml), want, JSON.stringify(yaml));
});

test("parseProperties strips quotes and unescapes what YAML escapes", () => {
  const cases: [string, Property[]][] = [
    ['title: "Hello: World"', [one("title", "Hello: World")]],
    ["author: 'Ana'", [one("author", "Ana")]],
    ['quote: "say \\"hi\\" \\\\ bye"', [one("quote", 'say "hi" \\ bye')]],
    ["it: 'it''s'", [one("it", "it's")]],
    ['x: "a # b" # a comment', [one("x", "a # b")]],
    ["x: ''", [empty("x")]],
    ['"quoted key": v', [one("quoted key", "v")]],
  ];
  for (const [yaml, want] of cases) assert.deepEqual(parseProperties(yaml), want, yaml);
});

test("parseProperties ignores comments and blank lines but keeps a # that is part of a value", () => {
  const yaml = "# top: not a key\n\ntitle: A # trailing\n\n   # indented\nurl: https://x.y/z#frag\ntag: #notmine\nlast: x";
  assert.deepEqual(parseProperties(yaml), [one("title", "A"), one("url", "https://x.y/z#frag"), empty("tag"), one("last", "x")]);
});

test("parseProperties shows an empty value as no values", () => {
  assert.deepEqual(parseProperties("empty:\nnext: x\nlast:   "), [empty("empty"), one("next", "x"), empty("last")]);
  assert.deepEqual(parseProperties("tags: # nothing yet\n"), [empty("tags")]);
});

test("parseProperties reads CRLF text, and a bare CR as a line break", () => {
  assert.deepEqual(parseProperties("tags:\r\n  - a\r\ntitle: T\r\n"), [list("tags", "a"), one("title", "T")]);
  assert.deepEqual(parseProperties("a: 1\rb: 2"), [one("a", "1"), one("b", "2")]);
});

test("parseProperties skips what it cannot show, along with its own lines", () => {
  const cases: [string, Property[]][] = [
    ["parent:\n  child: 1\n  other:\n    deep: 2\nafter: ok", [one("after", "ok")]],
    ["related:\n  - name: A\n    url: B\nafter: ok", [one("after", "ok")]],
    ["nested:\n  - [a, b]\nafter: ok", [one("after", "ok")]],
    ["text: |\n  one\n  two\nafter: ok", [one("after", "ok")]],
    ["text: >-\n  one\nafter: ok", [one("after", "ok")]],
    ["text: |\nafter: ok", [one("after", "ok")]], // a block scalar even with nothing in it
    ["text: >\nafter: ok", [one("after", "ok")]],
    ["text: one\n  two\nafter: ok", [one("after", "ok")]], // a value that runs on
    ["tags: [a,\n  b]\nafter: ok", [one("after", "ok")]],
    ["tags: [a, b\nafter: ok", [one("after", "ok")]],
    ["tags: [a,\nafter: ok", [one("after", "ok")]],
    ["tags: [a] b\nafter: ok", [one("after", "ok")]],
    ["tags: [a, {b: 1}]\nafter: ok", [one("after", "ok")]],
    ["tags: [[a], b]\nafter: ok", [one("after", "ok")]],
    ["obj: {a: 1}\nafter: ok", [one("after", "ok")]],
    ["x: \"unterminated\nafter: ok", [one("after", "ok")]],
    ["x: 'unterminated\nafter: ok", [one("after", "ok")]],
    ['x: "a" b\nafter: ok', [one("after", "ok")]],
    ["just some text\n- stray item\n: no key\nkey without colon\nurl:https://x.y\ngood: yes", [one("good", "yes")]],
    ["  indented: 1\nok: 2", [one("ok", "2")]],
    ["- a: b\nok: 1", [one("ok", "1")]], // a list item is not a key, whatever is in it
  ];
  for (const [yaml, want] of cases) assert.deepEqual(parseProperties(yaml), want, JSON.stringify(yaml));
});

test("parseProperties never throws, whatever the text", () => {
  for (const yaml of ["\u0000", ":", "::", ": :", "[[[", "]", "a: [", "a: \"", "a: '", "- - -", "-", "- ", "a:\n-", "\\", "#", "a: #", "\r", "\r\r\n\n", "a:b", "a: b: c", "\"", "'", "a:\n  - \"", "{", "}", "a: |", "a: >\n"]) {
    assert.doesNotThrow(() => parseProperties(yaml), JSON.stringify(yaml));
    assert.ok(Array.isArray(parseProperties(yaml)));
  }
});

test("parseProperties stays linear on a long run of spaces, as a synced file may hold anything", () => {
  const spaces = " ".repeat(200_000);
  const started = performance.now();
  parseProperties(`a${spaces}\ntags: [a,${spaces}b${spaces}\nb:${spaces}c${spaces}#${spaces}\n${spaces}:`);
  assert.ok(performance.now() - started < 1000, "quadratic scan");
});
