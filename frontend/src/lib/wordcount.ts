/** The words of a note's text: runs of non-space that hold a letter or a number, without the frontmatter. */
export function countWords(text: string): number {
  const body = text.replace(/^﻿?---[ \t]*\r?\n[\s\S]*?\r?\n---[ \t]*(?:\r?\n|$)/, "");
  let count = 0;
  for (const token of body.split(/\s+/)) if (/[\p{L}\p{N}]/u.test(token)) count++;
  return count;
}
