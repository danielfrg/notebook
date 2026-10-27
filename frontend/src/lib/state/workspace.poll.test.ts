import test from "node:test";
import assert from "node:assert/strict";

test("same revision is ignored, clean changes reload, and dirty changes stay in the editor", async () => {
  const keys = ["$state", "window", "document", "location", "localStorage"] as const;
  const previous = keys.map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const);
  const previousFetch = globalThis.fetch;
  const define = (key: string, value: unknown) => Object.defineProperty(globalThis, key, { configurable: true, value });
  define("$state", <T>(value: T) => value);
  const windowEvents = new EventTarget();
  define("window", windowEvents);
  define("document", { visibilityState: "visible" });
  const locationState = { hash: "#/notes/one.md", replace: () => {} };
  define("location", locationState);
  define("localStorage", { getItem: () => null, setItem: () => {} });
  let disk = { content: "initial", revision: "r1" };
  let noteReads = 0;
  let failListing = false;
  let holdRevision = false;
  let answerRevision!: (response: Response) => void;
  globalThis.fetch = async (input, init) => {
    const url = String(input);
    if (url.endsWith("/api/vaults")) return Response.json({ readOnly: false, vaults: [{ name: "notes" }] });
    if (url.includes("/files")) {
      if (failListing) return Response.json({ detail: "Vault temporarily unavailable" }, { status: 503 });
      return Response.json({ notes: [{ path: "one.md", size: disk.content.length, modified: "now" }], assets: [], folders: [], fingerprint: "f1" }, { headers: { ETag: '"f1"' } });
    }
    if (url.includes("/revision")) {
      if (url.includes("second.md")) return Response.json({ revision: "s1", modified: "now" });
      if (holdRevision) return new Promise<Response>((resolve) => { answerRevision = resolve; });
      return disk.revision ? Response.json({ revision: disk.revision, modified: "now" }) : new Response("", { status: 404 });
    }
    if (url.includes("/notes") && init?.method === "POST") {
      const input = JSON.parse(String(init.body)) as { path: string; content: string };
      disk = { content: input.content, revision: "r-created" };
      return Response.json({ path: input.path, ...disk, size: disk.content.length, modified: "now" }, { status: 201 });
    }
    if (url.includes("/note") && init?.method === "PUT") {
      const input = JSON.parse(String(init.body)) as { content: string; revision: string };
      if (!disk.revision) return Response.json({ detail: "Note not found" }, { status: 404 });
      if (input.revision !== disk.revision) return Response.json({ detail: "Changed on disk", errors: [{ value: disk.revision }] }, { status: 409 });
      disk = { content: input.content, revision: "r-saved" };
      return Response.json({ path: "one.md", ...disk, size: disk.content.length, modified: "now" });
    }
    if (url.includes("/note")) {
      noteReads++;
      if (url.includes("second.md")) return Response.json({ path: "second.md", content: "second note", revision: "s1", size: 11, modified: "now" });
      return Response.json({ path: "one.md", ...disk, size: disk.content.length, modified: "now" });
    }
    throw new Error(`unexpected ${url}`);
  };
  try {
    const { workspace } = await import("./workspace.svelte.ts");
    await workspace.init();
    assert.equal(workspace.value, "initial");
    await workspace.pollNow();
    assert.equal(noteReads, 1, "matching revision must not fetch the note");
    disk = { content: "shell change", revision: "r2" };
    await workspace.pollNow();
    assert.equal(workspace.value, "shell change");
    holdRevision = true;
    const oldNotePoll = workspace.pollNow();
    locationState.hash = "#/notes/second.md";
    windowEvents.dispatchEvent(new Event("hashchange"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    assert.equal(workspace.value, "second note");
    holdRevision = false;
    answerRevision(Response.json({ revision: "r2", modified: "now" }));
    await oldNotePoll;
    assert.equal(workspace.value, "second note", "a late answer for the old note must be discarded");
    locationState.hash = "#/notes/one.md";
    windowEvents.dispatchEvent(new Event("hashchange"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    assert.equal(workspace.value, "shell change");
    workspace.edit("local edit");
    disk = { content: "another shell change", revision: "r3" };
    await workspace.pollNow();
    assert.equal(workspace.value, "local edit");
    assert.equal(workspace.externalConflict, true);
    await workspace.reloadFromDisk();
    assert.equal(workspace.externalConflict, false);
    holdRevision = true;
    const stalePoll = workspace.pollNow();
    workspace.edit("saved locally");
    await workspace.saveNow();
    holdRevision = false;
    answerRevision(Response.json({ revision: "r3", modified: "now" }));
    await stalePoll;
    assert.equal(workspace.value, "saved locally");
    assert.equal(workspace.externalConflict, false, "a response started before save must be dropped");
    disk = { content: "", revision: "" };
    await workspace.pollNow();
    assert.equal(workspace.missingNote, true);
    await workspace.saveMissingAsNew("Recovered");
    assert.equal(workspace.path, "Recovered.md");
    assert.equal(workspace.value, "saved locally");
    assert.equal(workspace.missingNote, false);
    workspace.edit("overwrite this");
    disk = { content: "disk won", revision: "r-other" };
    await workspace.saveNow();
    assert.equal(workspace.saveState, "conflict");
    await workspace.overwriteDisk();
    assert.equal(disk.content, "overwrite this");
    assert.equal(workspace.saveState, "clean");
    workspace.edit("keep after deletion");
    disk = { content: "", revision: "" };
    await workspace.saveNow();
    assert.equal(workspace.missingNote, true);
    assert.equal(workspace.value, "keep after deletion");
    failListing = true;
    await workspace.saveMissingAsNew("After error");
    assert.equal(workspace.path, "After error.md", "a completed create must still navigate if listing refresh fails");
  } finally {
    const { workspace } = await import("./workspace.svelte.ts");
    workspace.dispose();
    for (const [key, descriptor] of previous) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else Reflect.deleteProperty(globalThis, key);
    }
    globalThis.fetch = previousFetch;
  }
});
