import test from "node:test";
import assert from "node:assert/strict";

test("file mutations preserve the current route, pause old-note polling, and move expansion paths", async () => {
  const keys = ["$state", "window", "document", "location", "localStorage"] as const;
  const previous = keys.map(
    (key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const,
  );
  const previousFetch = globalThis.fetch;
  const define = (key: string, value: unknown) =>
    Object.defineProperty(globalThis, key, { configurable: true, value });
  define("$state", <T>(value: T) => value);
  const events = new EventTarget();
  define("window", events);
  define("document", Object.assign(new EventTarget(), { visibilityState: "visible" }));
  const route = {
    hash: "#/notes/one.md",
    replace(hash: string) {
      this.hash = hash;
    },
  };
  define("location", route);
  define("localStorage", { getItem: () => null, setItem: () => {} });
  const notes = new Map([
    ["one.md", { content: "one", revision: "r1" }],
    ["second.md", { content: "second", revision: "r2" }],
  ]);
  const folders = new Set(["Folder", "Folder/Child"]);
  let holdMove = false;
  let moveCalls = 0;
  let finishMove!: () => void;
  let holdRemove = false;
  let finishRemove!: () => void;
  let holdFiles = false;
  const pendingFiles: ((response: Response) => void)[] = [];
  globalThis.fetch = async (input, init) => {
    const url = new URL(String(input), "http://local");
    const path = url.searchParams.get("path") ?? "";
    if (url.pathname === "/api/vaults")
      return Response.json({ readOnly: false, vaults: [{ name: "notes" }] });
    if (url.pathname.endsWith("/files")) {
      if (holdFiles)
        return new Promise<Response>((resolve) => {
          pendingFiles.push(resolve);
        });
      return Response.json({
        notes: [...notes].map(([entry, note]) => ({
          path: entry,
          size: note.content.length,
          modified: "now",
        })),
        assets: [],
        folders: [...folders],
        fingerprint: "f1",
      });
    }
    if (url.pathname.endsWith("/revision")) {
      const note = notes.get(path);
      return note
        ? Response.json({ revision: note.revision, modified: "now" })
        : new Response("", { status: 404 });
    }
    if (url.pathname.endsWith("/note")) {
      if (init?.method === "PUT")
        return Response.json({ detail: "save unavailable" }, { status: 503 });
      const note = notes.get(path);
      return note
        ? Response.json({ path, ...note, size: note.content.length, modified: "now" })
        : new Response("", { status: 404 });
    }
    if (url.pathname.endsWith("/move")) {
      moveCalls++;
      const { from, to } = JSON.parse(String(init?.body)) as { from: string; to: string };
      if (folders.has(from)) {
        for (const folder of [...folders])
          if (folder === from || folder.startsWith(`${from}/`)) {
            folders.delete(folder);
            folders.add(to + folder.slice(from.length));
          }
      } else {
        const note = notes.get(from)!;
        notes.delete(from);
        notes.set(to, note);
      }
      if (holdMove)
        await new Promise<void>((resolve) => {
          finishMove = resolve;
        });
      return Response.json({ path: to });
    }
    if (url.pathname.endsWith("/entry")) {
      notes.delete(path);
      if (holdRemove)
        await new Promise<void>((resolve) => {
          finishRemove = resolve;
        });
      return new Response(null, { status: 204 });
    }
    throw new Error(`unexpected ${url}`);
  };
  try {
    const { workspace } = await import("./workspace.svelte.ts");
    await workspace.init();
    holdMove = true;
    const rename = workspace.renameEntry("one.md", "renamed");
    await new Promise((resolve) => setTimeout(resolve, 0));
    await workspace.pollNow();
    assert.equal(workspace.missingNote, false, "the old path must not be polled during its rename");
    route.hash = "#/notes/second.md";
    events.dispatchEvent(new Event("hashchange"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    finishMove();
    await rename;
    assert.equal(
      workspace.path,
      "second.md",
      "a completed rename cannot navigate away from a newer route",
    );
    assert.equal(workspace.value, "second");

    holdRemove = true;
    const trash = workspace.trashEntry("second.md");
    await new Promise((resolve) => setTimeout(resolve, 0));
    route.hash = "#/notes/renamed.md";
    events.dispatchEvent(new Event("hashchange"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    finishRemove();
    await trash;
    assert.equal(workspace.path, "renamed.md", "a completed trash cannot clear a newer route");

    workspace.expanded = new Set(["Folder", "Folder/Child"]);
    holdMove = false;
    await workspace.renameEntry("Folder", "Renamed");
    assert.deepEqual([...workspace.expanded].sort(), ["Renamed", "Renamed/Child"]);
    holdFiles = true;
    const firstListing = workspace.pollNow();
    const secondListing = workspace.pollNow();
    assert.equal(pendingFiles.length, 2);
    const listing = (folder: string) =>
      Response.json({ notes: [], assets: [], folders: [folder], fingerprint: folder });
    pendingFiles[1](listing("Latest"));
    pendingFiles[0](listing("Stale"));
    await Promise.all([firstListing, secondListing]);
    assert.deepEqual(
      workspace.listing?.folders,
      ["Latest"],
      "an older listing cannot replace a newer one",
    );
    holdFiles = false;
    workspace.readOnly = true;
    await assert.rejects(
      workspace.overwriteDisk(),
      /read-only/i,
      "direct overwrite must be guarded without the server",
    );
    workspace.readOnly = false;
    workspace.edit("unsaved text");
    const movesBeforeFailure = moveCalls;
    await assert.rejects(workspace.renameEntry("renamed.md", "blocked"), /Resolve the open note/);
    assert.equal(moveCalls, movesBeforeFailure, "a failed save blocks the disk mutation");
    assert.equal(workspace.value, "unsaved text");
  } finally {
    (await import("./workspace.svelte.ts")).workspace.dispose();
    for (const [key, descriptor] of previous) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else Reflect.deleteProperty(globalThis, key);
    }
    globalThis.fetch = previousFetch;
  }
});
