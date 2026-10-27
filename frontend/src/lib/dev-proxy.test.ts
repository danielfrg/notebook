import test from "node:test";
import assert from "node:assert/strict";
import { createServer as createHTTPServer } from "node:http";
import { once } from "node:events";
import type { AddressInfo } from "node:net";
import { createServer as createViteServer } from "vite";

test("dev proxy preserves browser Host and Origin, including rejected foreign origins", async () => {
  // Stands in for the Go API guard: a mutating request needs X-Notebook: 1 and
  // an Origin that names the host the request arrived on.
  const upstream = createHTTPServer((request, response) => {
    if (
      request.method === "POST" &&
      (request.headers["x-notebook"] !== "1" ||
        request.headers.origin !== `http://${request.headers.host}`)
    ) {
      response.writeHead(403).end("Origin does not match the request host");
      return;
    }
    response.setHeader("Content-Type", "application/json");
    response.end(
      JSON.stringify({
        host: request.headers.host,
        origin: request.headers.origin,
        notebook: request.headers["x-notebook"],
        path: request.url,
      }),
    );
  });
  upstream.listen(0, "127.0.0.1");
  await once(upstream, "listening");
  const previousTarget = process.env.NOTEBOOK_API_TARGET;
  process.env.NOTEBOOK_API_TARGET = `http://127.0.0.1:${(upstream.address() as AddressInfo).port}`;
  let vite: Awaited<ReturnType<typeof createViteServer>> | undefined;
  try {
    // Import after the target is set: the config reads it when it is loaded.
    const { default: config } = await import("../../vite.config.ts");
    vite = await createViteServer({
      ...config,
      configFile: false,
      logLevel: "silent",
      server: { ...config.server, port: 0, strictPort: false, open: false },
    });
    await vite.listen();
    const host = `127.0.0.1:${(vite.httpServer!.address() as AddressInfo).port}`;
    const origin = `http://${host}`;

    const response = await fetch(`${origin}/api/vaults/x/move`, {
      method: "POST",
      headers: { Origin: origin, "Content-Type": "application/json", "X-Notebook": "1" },
      body: JSON.stringify({ from: "a.md", to: "b.md" }),
    });
    const body = await response.text();
    assert.equal(response.status, 200, body);
    assert.deepEqual(JSON.parse(body), {
      host,
      origin,
      notebook: "1",
      path: "/api/vaults/x/move",
    });

    // A foreign Origin must reach the API unchanged so that it can refuse it.
    const foreign = await fetch(`${origin}/api/vaults/x/move`, {
      method: "POST",
      headers: {
        Origin: "https://other.test",
        "Content-Type": "application/json",
        "X-Notebook": "1",
      },
      body: "{}",
    });
    assert.equal(foreign.status, 403);
  } finally {
    if (previousTarget === undefined) delete process.env.NOTEBOOK_API_TARGET;
    else process.env.NOTEBOOK_API_TARGET = previousTarget;
    await vite?.close();
    upstream.closeAllConnections();
    await new Promise<void>((resolve, reject) =>
      upstream.close((error) => (error ? reject(error) : resolve())),
    );
  }
});
