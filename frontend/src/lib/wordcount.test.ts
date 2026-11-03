import test from "node:test";
import assert from "node:assert/strict";
import { countWords } from "./wordcount.ts";

test("countWords counts words, not markers or frontmatter", () => {
  assert.equal(countWords(""), 0);
  assert.equal(countWords("# Title\n\nOne two  three.\n- four"), 5);
  assert.equal(countWords("---\ntags: [a b c]\n---\nhello world"), 2);
  assert.equal(countWords("---\r\ntitle: x\r\n---\r\nhello"), 1);
  assert.equal(countWords("--- not frontmatter\nword"), 3);
  assert.equal(countWords("***  ---  >"), 0);
});
