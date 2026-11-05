import test from "node:test";
import assert from "node:assert/strict";
import { ApiError } from "../api.ts";
import { nextRoute, readOnlyReason, splitBom, joinBom } from "./route-sync.ts";

test("nextRoute falls back to a known vault and clears the path on vault changes", () => {
  const vaults = ["notes", "work"];
  assert.deepEqual(
    nextRoute({ vault: "gone", path: "x.md" }, { type: "note", path: "a.md" }, vaults),
    { vault: "notes", path: null },
  );
  assert.deepEqual(
    nextRoute({ vault: "notes", path: "a.md" }, { type: "vault", name: "work" }, vaults),
    { vault: "work", path: null },
  );
  assert.deepEqual(
    nextRoute({ vault: "notes", path: "a.md" }, { type: "vault", name: "gone" }, vaults),
    { vault: "notes", path: null },
  );
});

test("nextRoute rejects invalid decoded vault and path parts", () => {
  const vaults = ["notes"];
  assert.deepEqual(
    nextRoute({ vault: "../bad", path: "x.md" }, { type: "note", path: "ok.md" }, vaults),
    { vault: "notes", path: null },
  );
  for (const path of ["../bad.md", ".hidden/a.md", "a//b.md", "a\\b.md", "bad\ud800.md"]) {
    assert.deepEqual(nextRoute({ vault: "notes", path: null }, { type: "note", path }, vaults), {
      vault: "notes",
      path: null,
    });
  }
  const longPath = ["é".repeat(120), "é".repeat(120), "é".repeat(20) + ".md"].join("/");
  assert.deepEqual(
    nextRoute({ vault: "notes", path: null }, { type: "note", path: longPath }, vaults),
    { vault: "notes", path: null },
  );
});

test("nextRoute keeps the open note when an invalid note path is requested", () => {
  const current = { vault: "notes", path: "folder/open.md" };
  assert.deepEqual(nextRoute(current, { type: "note", path: "../bad.md" }, ["notes"]), current);
  assert.deepEqual(nextRoute(current, { type: "note", path: "bad\ud800.md" }, ["notes"]), current);
});

test("readOnlyReason distinguishes size, encoding, and mixed line endings", () => {
  assert.match(readOnlyReason({ error: new ApiError("large", 413) }) ?? "", /large|size/i);
  assert.match(readOnlyReason({ error: new ApiError("invalid", 415) }) ?? "", /UTF-8/i);
  assert.match(readOnlyReason({ lineSeparator: "mixed" }) ?? "", /line endings/i);
  assert.equal(readOnlyReason({ error: new ApiError("missing", 404) }), null);
  assert.equal(readOnlyReason({ lineSeparator: "\r\n" }), null);
});

test("BOM is stripped once at the host boundary and rejoined for saves", () => {
  assert.deepEqual(splitBom("\ufeff\ufefftext"), { bom: "\ufeff", value: "\ufefftext" });
  assert.deepEqual(splitBom("text"), { bom: "", value: "text" });
  assert.equal(joinBom("\ufeff", "edited"), "\ufeffedited");
  assert.equal(joinBom("", "edited"), "edited");
});
