import test from "node:test";
import assert from "node:assert/strict";
import { termParts } from "./search-terms.ts";

test("termParts marks every term, ignoring case and accents", () => {
  assert.deepEqual(termParts("Música and music", "musica"), [
    { text: "Música", match: true },
    { text: " and music", match: false },
  ]);
  assert.deepEqual(termParts("a big dog", '"big dog"'), [
    { text: "a ", match: false },
    { text: "big dog", match: true },
  ]);
  assert.deepEqual(termParts("text", ""), [{ text: "text", match: false }]);
});
