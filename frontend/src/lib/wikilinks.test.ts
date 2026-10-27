import test from "node:test";
import assert from "node:assert/strict";
import { parseWikilink, resolveAsset, resolveNote, type Wikilink } from "./wikilinks.ts";

const link = (target: string, rest: Partial<Wikilink> = {}): Wikilink => ({
  target,
  heading: null,
  alias: null,
  embed: false,
  ...rest,
});

test("parseWikilink reads the target, heading, alias and embed marker", () => {
  const cases: [string, Wikilink][] = [
    ["[[Note]]", link("Note")],
    ["[[a/b#h|alias]]", link("a/b", { heading: "h", alias: "alias" })],
    ["[[Note#Heading]]", link("Note", { heading: "Heading" })],
    ["[[Note|alias]]", link("Note", { alias: "alias" })],
    ["![[img.png|300]]", link("img.png", { alias: "300", embed: true })],
    ["![[Note#Heading]]", link("Note", { heading: "Heading", embed: true })],
    ["[[¿Qué es el Estoicismo?]]", link("¿Qué es el Estoicismo?")],
    ["[[Dario Z - El amor - Clase 1|Clase 1]]", link("Dario Z - El amor - Clase 1", { alias: "Clase 1" })],
    ["[[ Note | alias ]]", link("Note", { alias: "alias" })],
    ["[[Note#a#b]]", link("Note", { heading: "a#b" })], // a path of headings stays whole
    ["[[Note#^abc123]]", link("Note", { heading: "^abc123" })], // a block reference
    ["[[100% #1]]", link("100%", { heading: "1" })], // as in Obsidian, a # in a name cannot be linked
    ["[[a|b|c]]", link("a", { alias: "b|c" })], // the first pipe splits
    ["[[Note\\|alias]]", link("Note", { alias: "alias" })], // a pipe escaped for a table cell
    ["[[Note#]]", link("Note")],
    ["[[Note|]]", link("Note")],
  ];
  for (const [source, want] of cases) assert.deepEqual(parseWikilink(source), want, source);
});

test("parseWikilink refuses anything that is not exactly one whole wikilink", () => {
  for (const source of [
    "",
    "[[a",
    "a]]",
    "[[]]",
    "[[ ]]",
    "![[]]",
    "[[|alias]]",
    "[[#h]]",
    "[a]",
    "![a](b)",
    "!![[a]]",
    " [[a]]",
    "[[a]] ",
    "[[a]]b]]",
    "[[a[b]]",
    "[[a,b [c]]]", // nor can brackets, which is why this one is not a link
    "[[a]]]",
    "[[a\nb]]",
  ]) {
    assert.equal(parseWikilink(source), null, JSON.stringify(source));
  }
});

type Case = [paths: string[], from: string, target: string, want: string | null];

/** Every case must also hold when the vault lists its files the other way round. */
function check(resolve: typeof resolveNote, cases: Case[]) {
  for (const [paths, from, target, want] of cases) {
    const label = `${target} from ${from || "the root"} in ${JSON.stringify(paths)}`;
    assert.equal(resolve(paths, from, target), want, label);
    assert.equal(resolve([...paths].reverse(), from, target), want, `reversed: ${label}`);
  }
}

test("resolveNote finds a note by path, by file name or by the end of its path", () => {
  check(resolveNote, [
    // an exact path, with or without .md
    [["a/b.md", "c.md"], "x.md", "a/b.md", "a/b.md"],
    [["a/b.md", "c.md"], "x.md", "a/b", "a/b.md"],
    // a bare name is a file name anywhere in the vault, in any case, with or without .md
    [["Notes/Stoicism.md"], "x.md", "stoicism", "Notes/Stoicism.md"],
    [["Notes/Stoicism.md"], "x.md", "STOICISM.MD", "Notes/Stoicism.md"],
    [["Notes/Stoicism.md"], "x.md", "  Stoicism ", "Notes/Stoicism.md"],
    [["Release v1.2.md"], "x.md", "Release v1.2", "Release v1.2.md"],
    // names that are ordinary files
    [["a/¿Qué es el Estoicismo?.md"], "x.md", "¿qué es el estoicismo?", "a/¿Qué es el Estoicismo?.md"],
    [["100% #1.md", "100.md"], "x.md", "100% #1", "100% #1.md"],
    [["a,b [c].md"], "x.md", "a,b [c]", "a,b [c].md"],
    [["Dario Z - El amor - Clase 1.md"], "x.md", "Dario Z - El amor - Clase 1", "Dario Z - El amor - Clase 1.md"],
    // composed and decomposed accents are the same name
    [["Café.md"], "x.md", "Café", "Café.md"],
    // folder/Note matches the end of a path, on whole segments
    [["x/folder/Note.md", "other/Note.md"], "y.md", "folder/Note", "x/folder/Note.md"],
    [["xfolder/Note.md"], "y.md", "folder/Note", null],
    [["Note.md", "a/Note.md"], "y.md", "a/Note", "a/Note.md"],
  ]);
});

test("resolveNote breaks ties by the folder of the linking note, then the fewest segments, then the alphabet", () => {
  check(resolveNote, [
    [["a/Note.md", "b/Note.md"], "b/x.md", "Note", "b/Note.md"],
    [["a/Note.md", "b/Note.md"], "A/x.md", "Note", "a/Note.md"], // the folder is compared in any case
    [["Note.md", "a/Note.md"], "a/x.md", "Note", "a/Note.md"], // the folder beats fewer segments
    [["Note.md", "a/Note.md"], "b/x.md", "Note", "Note.md"],
    [["a/b/Note.md", "c/Note.md"], "z/x.md", "Note", "c/Note.md"],
    [["b/Note.md", "a/Note.md"], "z/x.md", "Note", "a/Note.md"],
    [["B/Note.md", "a/Note.md"], "z/x.md", "Note", "a/Note.md"], // the alphabet ignores case
    [["a/Note.md", "A/note.md"], "z/x.md", "Note", "A/note.md"], // and a tie left is settled by the path itself
  ]);
});

test("resolveNote returns null when nothing matches", () => {
  check(resolveNote, [
    [["a.md"], "x.md", "nope", null],
    [[], "x.md", "a", null],
    [["Notebook.md"], "x.md", "Note", null], // a prefix of a name is not the name
    [["My Note.md"], "x.md", "Note", null], // nor is the end of one
    [["a.md"], "x.md", "", null],
    [["a.md"], "x.md", "   ", null],
    [["a.md"], "x.md", ".md", null],
    [["pic.png"], "x.md", "pic.png", null], // an image is not a note
  ]);
});

test("resolveAsset follows the same rules on images, whose name keeps its extension", () => {
  check(resolveAsset, [
    [["img/pic.png"], "x.md", "pic.png", "img/pic.png"],
    [["img/pic.png"], "x.md", "PIC.PNG", "img/pic.png"],
    [["img/pic.png"], "x.md", "pic", null], // there is no .md to leave out
    [["x/img/pic.png", "y/pic.png"], "q.md", "img/pic.png", "x/img/pic.png"], // the end of a path
    [["img/pic.png", "a/Pic.PNG"], "a/x.md", "pic.png", "a/Pic.PNG"], // the folder of the note
    [["img/pic.png", "pic.png"], "z/x.md", "pic.png", "pic.png"], // fewest segments
    [["b/pic.png", "a/pic.png"], "z/x.md", "pic.png", "a/pic.png"], // the alphabet
    [["img/100% #1 (copy).png"], "x.md", "100% #1 (copy).png", "img/100% #1 (copy).png"],
    [["img/pic.png"], "x.md", "nope.png", null],
    [[], "x.md", "pic.png", null],
  ]);
});
