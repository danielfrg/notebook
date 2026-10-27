/**
 * The line break a note uses, so that the editor joins its lines with the same
 * one and saving changes nothing else: "\n" also for a text without any line
 * break, and "mixed" for a text with both LF and CRLF or with a bare CR, which
 * an editor state cannot keep as they are. A byte order mark plays no part.
 */
export function detectLineSeparator(text: string): "\n" | "\r\n" | "mixed" {
  let lf = false;
  let crlf = false;
  for (const [lineBreak] of text.matchAll(/\r\n|\r|\n/g)) {
    if (lineBreak === "\r") return "mixed";
    if (lineBreak === "\n") lf = true;
    else crlf = true;
    if (lf && crlf) return "mixed";
  }
  return crlf ? "\r\n" : "\n";
}
