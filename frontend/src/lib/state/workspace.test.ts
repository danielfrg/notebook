import test from "node:test";
import assert from "node:assert/strict";

test("disposed workspace can initialize again and restore its route listener", async () => {
  const keys = ["$state", "window", "location", "localStorage"] as const;
  const previous = keys.map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const);
  const previousFetch = globalThis.fetch;
  const calls: string[] = [];
  const listeners: string[] = [];
  const define = (key: string, value: unknown) => Object.defineProperty(globalThis, key, { configurable: true, value });
  define("$state", <T>(value: T) => value);
  define("window", {
    addEventListener: (type: string) => { listeners.push(`add:${type}`); },
    removeEventListener: (type: string) => { listeners.push(`remove:${type}`); },
  });
  define("location", { hash: "#/notes", replace: () => {} });
  define("localStorage", { getItem: () => null, setItem: () => {} });
  globalThis.fetch = async (input) => {
    calls.push(String(input));
    const body = String(input).endsWith("/api/vaults")
      ? { readOnly: false, vaults: [{ name: "notes" }] }
      : { notes: [], folders: [], assets: [], fingerprint: "synthetic" };
    return Response.json(body);
  };

  try {
    const { workspace } = await import("./workspace.svelte.ts");
    await workspace.init();
    workspace.dispose();
    await workspace.init();
    assert.equal(calls.filter((url) => url.endsWith("/api/vaults")).length, 2);
    assert.deepEqual(listeners, ["add:hashchange", "add:focus", "remove:hashchange", "remove:focus", "add:hashchange", "add:focus"]);
    workspace.dispose();
  } finally {
    (await import("./workspace.svelte.ts")).workspace.dispose();
    for (const [key, descriptor] of previous) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else Reflect.deleteProperty(globalThis, key);
    }
    globalThis.fetch = previousFetch;
  }
});
