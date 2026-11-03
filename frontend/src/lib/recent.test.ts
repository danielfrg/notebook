import test from "node:test";
import assert from "node:assert/strict";
import { groupRecent } from "./recent.ts";

test("groupRecent sorts newest first and groups by day, week and month", () => {
  const now = new Date(2026, 10, 3, 15, 0);
  const at = (month: number, day: number, hour = 9) => new Date(2026, month, day, hour).toISOString();
  const groups = groupRecent([
    { path: "old.md", modified: at(8, 2) },
    { path: "yesterday.md", modified: at(10, 2) },
    { path: "today.md", modified: at(10, 3) },
    { path: "week.md", modified: at(10, 1) },
    { path: "oct.md", modified: at(9, 5) },
  ], now, "en-US");
  assert.deepEqual(groups, [
    { label: "Today", paths: ["today.md"] },
    { label: "Yesterday", paths: ["yesterday.md"] },
    { label: "Previous 7 days", paths: ["week.md"] },
    { label: "October 2026", paths: ["oct.md"] },
    { label: "September 2026", paths: ["old.md"] },
  ]);
});
