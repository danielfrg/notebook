/** Notes newest first, in the groups the Recent view shows: Today, Yesterday, the last week, then by month. */
export type RecentGroup = { label: string; paths: string[] };

const DAY = 86_400_000;

export function groupRecent(
  notes: readonly { path: string; modified: string }[],
  now: Date = new Date(),
  locale?: string,
): RecentGroup[] {
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const groups: RecentGroup[] = [];
  const sorted = notes.toSorted((a, b) => Date.parse(b.modified) - Date.parse(a.modified));
  for (const note of sorted) {
    const time = Date.parse(note.modified);
    const date = new Date(Number.isNaN(time) ? 0 : time);
    const label =
      time >= startOfToday
        ? "Today"
        : time >= startOfToday - DAY
          ? "Yesterday"
          : time >= startOfToday - 7 * DAY
            ? "Previous 7 days"
            : date.toLocaleDateString(locale, { month: "long", year: "numeric" });
    const last = groups.at(-1);
    if (last?.label === label) last.paths.push(note.path);
    else groups.push({ label, paths: [note.path] });
  }
  return groups;
}
