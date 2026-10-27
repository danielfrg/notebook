import test from "node:test";
import assert from "node:assert/strict";
import { parseWikilink, resolveAsset, resolveNote, wikilinkCreation, type Wikilink } from "./wikilinks.ts";

test("unresolved nested wikilink creation retains its vault-relative folder", () => {
  assert.deepEqual(wikilinkCreation("Home.md", "Projects/New"), { folder: "Projects", initial: "New" });
  assert.deepEqual(wikilinkCreation("Notes/Home.md", "Missing/Deep/New.md"), { folder: "Missing/Deep", initial: "New" });
  assert.deepEqual(wikilinkCreation("Notes/Home.md", "New"), { folder: "Notes", initial: "New" });
});

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
    ["[[Course Z - On love - Lesson 1|Lesson 1]]", link("Course Z - On love - Lesson 1", { alias: "Lesson 1" })],
    ["[[ Note | alias ]]", link("Note", { alias: "alias" })],
    ["[[Note#a#b]]", link("Note", { heading: "a#b" })], // a path of headings stays whole
    ["[[Note#^abc123]]", link("Note", { heading: "^abc123" })], // a block reference
    ["[[100% #1]]", link("100%", { heading: "1" })], // as in Obsidian, a # in a name cannot be linked
    ["[[a|b|c]]", link("a", { alias: "b|c" })], // the first pipe splits
    ["[[Note\\|alias]]", link("Note", { alias: "alias" })], // a pipe escaped for a table cell
    ["[[Note#]]", link("Note")],
    ["[[Note|]]", link("Note")],
    // a link inside the note itself has no target
    ["[[#Heading]]", link("", { heading: "Heading" })],
    ["[[#^abc123]]", link("", { heading: "^abc123" })],
    ["[[ #Heading | alias ]]", link("", { heading: "Heading", alias: "alias" })],
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
    "[[#]]", // neither a note nor a heading
    "[[ # ]]",
    "[[#|alias]]",
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
    [["Course Z - On love - Lesson 1.md"], "x.md", "Course Z - On love - Lesson 1", "Course Z - On love - Lesson 1.md"],
    // composed and decomposed accents are the same name
    [["Cafe\u0301.md"], "x.md", "Caf\u00e9", "Cafe\u0301.md"],
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

test("resolveNote takes the exact path from the vault root over any other match, for a target with a folder", () => {
  const both = ["a/Note.md", "x/a/Note.md"];
  check(resolveNote, [
    // the linking note sits beside the longer path, which is not the path the target spells
    [both, "x/a/other.md", "a/Note", "a/Note.md"],
    [both, "x/a/other.md", "a/Note.md", "a/Note.md"],
    [both, "x/a/other.md", "A/note", "a/Note.md"], // in any case
    [["a/b/Note.md", "x/a/b/Note.md"], "x/a/b/other.md", "a/b/Note", "a/b/Note.md"],
    // from the folder above, the exact path is also the shortest
    [both, "x/other.md", "a/Note", "a/Note.md"],
    [both, "x/other.md", "a/Note.md", "a/Note.md"],
    [both, "x/other.md", "A/note", "a/Note.md"],
    // without an exact path the usual order applies to the ends of paths
    [["x/a/Note.md", "y/a/Note.md"], "y/a/other.md", "a/Note", "y/a/Note.md"],
    [["x/a/Note.md", "y/a/Note.md"], "z/other.md", "a/Note", "x/a/Note.md"],
    // two exact paths that differ in case alone are settled by the usual order
    [["a/Note.md", "A/note.md"], "q.md", "a/note", "A/note.md"],
    // a bare name has no folder to be exact about: a note at the root gets no preference
    [["Note.md", "a/Note.md"], "a/x.md", "Note", "a/Note.md"],
    [["Note.md", "a/Note.md"], "a/x.md", "Note.md", "a/Note.md"],
  ]);
});

test("resolveNote returns null when nothing matches", () => {
  check(resolveNote, [
    [["a.md"], "x.md", "nope", null],
    [[], "x.md", "a", null],
    [["Notebook.md"], "x.md", "Note", null], // a prefix of a name is not the name
    [["My Note.md"], "x.md", "Note", null], // nor is the end of one
    [["a.md"], "x.md", "", null], // the target of [[#Heading]], which names the note itself
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

test("resolveAsset takes the exact path from the vault root first too", () => {
  const both = ["img/pic.png", "x/img/pic.png"];
  check(resolveAsset, [
    [both, "x/img/other.md", "img/pic.png", "img/pic.png"],
    [both, "x/img/other.md", "IMG/Pic.PNG", "img/pic.png"], // in any case
    [both, "x/other.md", "img/pic.png", "img/pic.png"],
    [["x/img/pic.png", "y/img/pic.png"], "y/img/other.md", "img/pic.png", "y/img/pic.png"], // no exact path: the usual order
    [["pic.png", "a/pic.png"], "a/x.md", "pic.png", "a/pic.png"], // a bare name: no preference for the root
  ]);
});
