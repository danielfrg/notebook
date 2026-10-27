import assert from "node:assert/strict";
import { test } from "node:test";
import type { ApiClient, SearchHit } from "../api.ts";

test("search debounces, ignores stale responses, and ignores replies after dispose", async () => {
  const keys = ["$state", "window", "setTimeout", "clearTimeout"] as const;
  const previous = keys.map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const);
  const define = (key: string, value: unknown) => Object.defineProperty(globalThis, key, { configurable: true, value });
  const timers = new Map<number, () => void>();
  let nextTimer = 0;
  const searches: { query: string; resolve: (hits: SearchHit[]) => void }[] = [];
  const hit = (path: string): SearchHit => ({ path, title: path, score: 1, modified: "now", snippets: [{ line: 3, text: path }] });
  define("$state", <T>(value: T) => value);
  define("window", new EventTarget());
  define("setTimeout", (callback: () => void) => { timers.set(++nextTimer, callback); return nextTimer; });
  define("clearTimeout", (id: number) => { timers.delete(id); });

  try {
    const { workspace } = await import("./workspace.svelte.ts");
    workspace.dispose();
    workspace.vault = "scratch";
    (workspace as unknown as { api: ApiClient }).api = {
      search: (_vault, query) => new Promise<SearchHit[]>((resolve) => { searches.push({ query, resolve }); }),
    } as ApiClient;
    const fire = () => {
      const [id, callback] = [...timers][0]!;
      timers.delete(id);
      callback();
    };

    workspace.setSearchQuery("old");
    assert.equal(workspace.searchLoading, true);
    workspace.setSearchQuery("first");
    assert.equal(timers.size, 1, "the first debounce timer was cancelled");
    fire();
    assert.deepEqual(searches.map(({ query }) => query), ["first"]);

    workspace.setSearchQuery("second");
    searches[0]!.resolve([hit("stale.md")]);
    await new Promise<void>((resolve) => setImmediate(resolve));
    assert.deepEqual(workspace.searchHits, []);
    assert.equal(workspace.searchLoading, true);

    fire();
    searches[1]!.resolve([hit("current.md")]);
    await new Promise<void>((resolve) => setImmediate(resolve));
    assert.deepEqual(workspace.searchHits.map(({ path }) => path), ["current.md"]);
    assert.equal(workspace.searchLoading, false);

    workspace.setSearchQuery("late");
    fire();
    workspace.dispose();
    searches[2]!.resolve([hit("after-dispose.md")]);
    await new Promise<void>((resolve) => setImmediate(resolve));
    assert.deepEqual(workspace.searchHits, []);
  } finally {
    const { workspace } = await import("./workspace.svelte.ts");
    workspace.dispose();
    for (const [key, descriptor] of previous) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else Reflect.deleteProperty(globalThis, key);
    }
  }
});
