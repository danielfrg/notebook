import test from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

/*
 * The theme contract (spec, Shell): src/theme.css is the only file with
 * palette values, so replacing it restyles the app without other edits. This
 * fails on a color literal in any other .css, .svelte or .ts file under src
 * (an editor theme is code too), tests aside, and says where: file, line and
 * literal.
 *
 * The one exception is pure black or white, as #000 or #fff, inside a
 * color-mix() call. kit-ui's theming doc permits them as shade math (darken a
 * token on hover, derive an ink from an accent): they are arithmetic
 * constants, not palette entries, and the result still follows the token.
 * Every other literal is a finding, also another color inside color-mix() and
 * a literal in a var() fallback (which kit-ui's own raw-color rule lets
 * through). Named colors (red) are not scanned, nor is public/favicon.svg.
 */

type Finding = { line: number; literal: string };

// Hex colors (#rgb, #rgba, #rrggbb, #rrggbbaa) and the color functions. A hex
// literal does not follow a word character, "-" or "&" (a URL fragment, an
// HTML entity such as &#169;) and does not run into one (an id such as
// #add-note). "color-mix(" does not match: "color" is not followed by "(".
const LITERAL =
  /(?<![\w&-])#(?:[0-9a-f]{8}|[0-9a-f]{6}|[0-9a-f]{3,4})(?![\w-])|(?<![\w-])(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\(/gi;
const SHADE_CONSTANT = /^#(?:000|000000|fff|ffffff)$/i;

/** The [start, end) of every color-mix(...) call that closes, nested ones included. */
function colorMixCalls(text: string): [number, number][] {
  const calls: [number, number][] = [];
  for (const match of text.matchAll(/color-mix\(/gi)) {
    let depth = 1;
    let end = match.index + match[0].length;
    for (; end < text.length && depth > 0; end++) {
      if (text[end] === "(") depth++;
      else if (text[end] === ")") depth--;
    }
    if (depth === 0) calls.push([match.index, end]);
  }
  return calls;
}

function findColorLiterals(text: string): Finding[] {
  const calls = colorMixCalls(text);
  const findings: Finding[] = [];
  for (const match of text.matchAll(LITERAL)) {
    const inColorMix = calls.some(([start, end]) => start < match.index && match.index < end);
    if (inColorMix && SHADE_CONSTANT.test(match[0])) continue;
    findings.push({ line: text.slice(0, match.index).split("\n").length, literal: match[0] });
  }
  return findings;
}

test("the scanner finds every kind of color literal and nothing else", () => {
  const found: [source: string, literals: string[]][] = [
    ["a { color: #fff; }", ["#fff"]],
    ["a { color: #FFFFFF; }", ["#FFFFFF"]],
    ["a { color: #abcd; }", ["#abcd"]],
    ["a { color: #11223344; }", ["#11223344"]],
    ["a { color: rgb(0 0 0); }", ["rgb("]],
    ["a { color: RGBA(0, 0, 0, .5); }", ["RGBA("]],
    ["a { color: hsl(0 0% 0%); }", ["hsl("]],
    ["a { color: hsla(0, 0%, 0%, .5); }", ["hsla("]],
    ["a { color: hwb(0 0% 0%); }", ["hwb("]],
    ["a { color: lab(50% 0 0); }", ["lab("]],
    ["a { color: lch(50% 0 0); }", ["lch("]],
    ["a { color: oklab(50% 0 0); }", ["oklab("]],
    ["a { color: oklch(50% 0 0); }", ["oklch("]],
    ["a { color: color(display-p3 1 0 0); }", ["color("]],
    ['<path fill="#c00" />', ["#c00"]],
    ["const color = '#c00';", ["#c00"]],
    ["a { b: #111; c: #222 }", ["#111", "#222"]],
    // Inside color-mix() only a pure black or white is a shade constant.
    ["a { c: color-mix(in srgb, var(--x), #123456); }", ["#123456"]],
    ["a { c: color-mix(in srgb, var(--x), rgb(0 0 0)); }", ["rgb("]],
    ["a { c: color-mix(in srgb, var(--x), #000000ff); }", ["#000000ff"]],
    // Outside it they are palette entries, as a var() fallback too.
    ["a { c: #000; }", ["#000"]],
    ["a { c: color-mix(in srgb, red, blue) #fff; }", ["#fff"]],
    ["a { c: color-mix(in srgb, red, blue; d: #fff; }", ["#fff"]], // a call that never closes
    ["a { c: var(--x, #fff); }", ["#fff"]],
  ];
  for (const [source, literals] of found) {
    assert.deepEqual(
      findColorLiterals(source).map((finding) => finding.literal),
      literals,
      source,
    );
  }

  const allowed = [
    "a { c: color-mix(in srgb, var(--x) 80%, #000); }",
    "a { c: color-mix(in srgb, var(--x) 80%, #FFF); }",
    "a { c: color-mix(in srgb, var(--x), #ffffff 10%); }",
    "a { c: color-mix(in srgb, var(--x), #000000); }",
    "a { c: color-mix(\n  in srgb,\n  var(--x) 20%,\n  #000\n); }",
    "a { c: color-mix(in srgb, color-mix(in srgb, var(--x), #fff) 50%, #000); }",
    "a { c: color-mix(in oklch, var(--x) 24%, transparent); }", // a color space, not a color
    "a { color: var(--text-primary); background: transparent; }",
    ".x { white-space: nowrap; }",
    '<a href="#main">Skip</a>',
    '<a href="#add-note">Add</a>',
    "<p>&#169; &#8212; page#fff</p>",
    "/* theme tokens only */",
  ];
  for (const source of allowed) assert.deepEqual(findColorLiterals(source), [], source);
});

/**
 * Whether a file, named by its path under src, is held to the contract. Only
 * src/theme.css is exempt, and the tests, whose fixtures may hold any text: a
 * theme.css anywhere else is a stylesheet like any other.
 */
const isScanned = (file: string): boolean =>
  (/\.(css|svelte)$/.test(file) && file !== "theme.css") || (file.endsWith(".ts") && !file.endsWith(".test.ts"));

test("every stylesheet, component and script is held to the contract, except src/theme.css and the tests", () => {
  for (const [file, scanned] of [
    ["app.css", true],
    ["App.svelte", true],
    ["lib/components/Tree.svelte", true],
    ["lib/editor.css", true],
    ["lib/theme.css", true],
    ["lib/deep/theme.css", true],
    ["main.ts", true],
    ["lib/paths.ts", true],
    ["lib/editor/theme.ts", true],
    ["theme.css", false],
    ["lib/paths.test.ts", false],
    ["lib/editor/markdown.test.ts", false],
    ["lib/notes.css.md", false],
  ] as const) {
    assert.equal(isScanned(file), scanned, file);
  }
});

test("the scanner reports the line of each literal", () => {
  const text = "a {\n  color: var(--x);\n  background: #fff;\n}\nb {\n  fill: rgb(1 2 3);\n}";
  assert.deepEqual(findColorLiterals(text), [
    { line: 3, literal: "#fff" },
    { line: 6, literal: "rgb(" },
  ]);
});

test("theme.css is the only file with palette values", () => {
  const src = fileURLToPath(new URL("..", import.meta.url));
  const files = readdirSync(src, { recursive: true, encoding: "utf8" }).filter(isScanned).sort();
  // The scan has to see the files that carry the layout, the shell and the scripts.
  assert.ok(
    files.includes("app.css") && files.includes("App.svelte") && files.includes("main.ts"),
    `scanned: ${files.join(", ")}`,
  );

  const findings = files.flatMap((file) =>
    findColorLiterals(readFileSync(src + file, "utf8")).map(
      ({ line, literal }) => `${file}:${line}: ${literal}`,
    ),
  );
  const html = readFileSync(new URL("../../index.html", import.meta.url), "utf8");
  for (const style of html.matchAll(/<style\b[^>]*>([\s\S]*?)<\/style>/gi)) {
    findings.push(...findColorLiterals(style[1]).map(({ line, literal }) => `index.html:<style>+${line}: ${literal}`));
  }
  assert.equal(
    findings.length,
    0,
    `color literals outside theme.css (use a theme token, or move the value there):\n  ${findings.join("\n  ")}`,
  );
});
