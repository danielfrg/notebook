import test from "node:test";
import assert from "node:assert/strict";
import { STATUS_CODES } from "node:http";
import { ApiError, ConflictError, createApi } from "./api.ts";
import type { ApiClient } from "./api.ts";

type Call = { method: string; url: string; headers: Headers; body: string | undefined };

/** The client over a fetch that records each request and answers it with answer(). */
function setup(answer: (call: Call) => Response | Promise<Response>, base?: string) {
  const calls: Call[] = [];
  const fakeFetch: typeof fetch = async (input, init) => {
    const call: Call = {
      method: init?.method ?? "GET",
      url: String(input),
      headers: new Headers(init?.headers),
      body: typeof init?.body === "string" ? init.body : undefined,
    };
    calls.push(call);
    return answer(call);
  };
  return { api: createApi(fakeFetch, base), calls };
}

const ok = (body: unknown, init: ResponseInit = {}) =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });

/** An error as the server writes it (Huma problem JSON, without $schema). */
const problem = (status: number, detail: string, errors?: unknown[]) =>
  new Response(JSON.stringify({ title: STATUS_CODES[status], status, detail, errors }), {
    status,
    headers: { "Content-Type": "application/problem+json" },
  });

/** What the guard of the server asks of a stale save: 409 with the revision on disk in errors[0].value. */
const stale = (revision: unknown) =>
  problem(409, "note changed on disk", [
    { message: "revision of the note on disk", location: "body.revision", value: revision },
  ]);

const note = {
  path: "a.md",
  content: "# A\n",
  revision: "r1",
  size: 4,
  modified: "2026-10-26T10:00:00Z",
};
const listing = {
  notes: [{ path: "a.md", size: 4, modified: note.modified }],
  assets: [],
  folders: [],
  fingerprint: "fp1",
};
const hits = [
  {
    path: "a.md",
    title: "a",
    score: 3,
    modified: note.modified,
    snippets: [{ line: 2, text: "x" }],
  },
];

// What a request carries: a read nothing, a change X-Notebook, and a body its JSON type.
const READ = {};
const CHANGE = { "x-notebook": "1" };
const CHANGE_WITH_BODY = { "x-notebook": "1", "content-type": "application/json" };

type Operation = {
  name: string;
  run: (api: ApiClient) => Promise<unknown>;
  answer: () => Response;
  request: { method: string; url: string; headers: Record<string, string>; body?: unknown };
  result: unknown;
};

const operations: Operation[] = [
  {
    name: "vaults",
    run: (api) => api.vaults(),
    answer: () => ok({ readOnly: true, vaults: [{ name: "notes" }] }),
    request: { method: "GET", url: "/api/vaults", headers: READ },
    result: { readOnly: true, vaults: [{ name: "notes" }] },
  },
  {
    name: "files",
    run: (api) => api.files("notes"),
    answer: () => ok(listing, { headers: { ETag: '"fp1"' } }),
    request: { method: "GET", url: "/api/vaults/notes/files", headers: READ },
    result: { notModified: false, listing, etag: '"fp1"' },
  },
  {
    name: "note",
    run: (api) => api.note("notes", "a b/c.md"),
    answer: () => ok(note),
    request: { method: "GET", url: "/api/vaults/notes/note?path=a%20b%2Fc.md", headers: READ },
    result: note,
  },
  {
    name: "revision",
    run: (api) => api.revision("notes", "a.md"),
    answer: () => ok({ revision: "r1", modified: note.modified }),
    request: { method: "GET", url: "/api/vaults/notes/revision?path=a.md", headers: READ },
    result: { revision: "r1", modified: note.modified },
  },
  {
    name: "search",
    run: (api) => api.search("notes", "ab c"),
    answer: () => ok({ hits }),
    request: { method: "GET", url: "/api/vaults/notes/search?q=ab%20c", headers: READ },
    result: hits,
  },
  {
    name: "save",
    run: (api) => api.save("notes", "a.md", "# B\n", "r1"),
    answer: () => ok(note),
    request: {
      method: "PUT",
      url: "/api/vaults/notes/note?path=a.md",
      headers: CHANGE_WITH_BODY,
      body: { content: "# B\n", revision: "r1" },
    },
    result: note,
  },
  {
    name: "create",
    run: (api) => api.create("notes", "new.md", "x"),
    answer: () => ok(note, { status: 201 }),
    request: {
      method: "POST",
      url: "/api/vaults/notes/notes",
      headers: CHANGE_WITH_BODY,
      body: { path: "new.md", content: "x" },
    },
    result: note,
  },
  {
    name: "create without a text",
    run: (api) => api.create("notes", "new.md"),
    answer: () => ok(note, { status: 201 }),
    request: {
      method: "POST",
      url: "/api/vaults/notes/notes",
      headers: CHANGE_WITH_BODY,
      body: { path: "new.md" },
    },
    result: note,
  },
  {
    name: "mkdir",
    run: (api) => api.mkdir("notes", "a/b"),
    answer: () => ok({ path: "a/b" }, { status: 201 }),
    request: {
      method: "POST",
      url: "/api/vaults/notes/folders",
      headers: CHANGE_WITH_BODY,
      body: { path: "a/b" },
    },
    result: undefined,
  },
  {
    name: "move",
    run: (api) => api.move("notes", "a.md", "b.md"),
    answer: () => ok({ path: "b.md" }),
    request: {
      method: "POST",
      url: "/api/vaults/notes/move",
      headers: CHANGE_WITH_BODY,
      body: { from: "a.md", to: "b.md" },
    },
    result: { path: "b.md" },
  },
  {
    name: "remove",
    run: (api) => api.remove("notes", "a.md"),
    answer: () => new Response(null, { status: 204 }),
    // No body, so the guard asks for no content type.
    request: { method: "DELETE", url: "/api/vaults/notes/entry?path=a.md", headers: CHANGE },
    result: undefined,
  },
];

test("each operation makes the request the server expects and returns what it sent", async () => {
  for (const { name, run, answer, request, result } of operations) {
    const { api, calls } = setup(answer);
    assert.deepEqual(await run(api), result, name);
    assert.equal(calls.length, 1, name);
    const [call] = calls;
    assert.equal(call.method, request.method, name);
    assert.equal(call.url, request.url, name);
    assert.deepEqual(Object.fromEntries(call.headers), request.headers, name);
    assert.deepEqual(
      call.body === undefined ? undefined : JSON.parse(call.body),
      request.body,
      name,
    );
  }
});

test("a path travels in the path query, encoded, and the server reads back the name that was sent", async () => {
  const names = [
    "¿Qué es el Estoicismo?.md",
    "100% #1.md",
    "a,b [c].md",
    "a+b.md", // a + that is not escaped is read as a space
    "a b/c&d=e.md",
    "日本語/ノート.md",
    "50%2F50.md",
  ];
  for (const name of names) {
    const reads: [string, (api: ApiClient) => Promise<unknown>][] = [
      ["note", (api) => api.note("notes", name)],
      ["revision", (api) => api.revision("notes", name)],
      ["note", (api) => api.save("notes", name, "x", "r1")],
      ["entry", (api) => api.remove("notes", name)],
    ];
    for (const [endpoint, run] of reads) {
      const { api, calls } = setup(() => ok(note));
      await run(api);
      const url = `/api/vaults/notes/${endpoint}?path=${encodeURIComponent(name)}`;
      assert.equal(calls[0].url, url, name);
      assert.equal(new URL(url, "http://host").searchParams.get("path"), name);
    }
    const { api } = setup(() => ok(note));
    assert.equal(
      api.assetUrl("notes", name),
      `/api/vaults/notes/raw?path=${encodeURIComponent(name)}`,
    );
  }

  // Names that go in a body are not escaped, for JSON does that.
  for (const name of names) {
    const { api, calls } = setup(() => ok({ path: name }));
    await api.create("notes", name, "x");
    await api.mkdir("notes", name);
    await api.move("notes", name, `sub/${name}`);
    assert.deepEqual(
      calls.map((call) => JSON.parse(call.body!)),
      [{ path: name, content: "x" }, { path: name }, { from: name, to: `sub/${name}` }],
    );
  }
});

test("the name of a vault is encoded in the URL, and base goes in front of every URL", async () => {
  const { api, calls } = setup(() => ok(note), "http://127.0.0.1:8080");
  await api.note("my vault/ü", "a.md");
  assert.equal(calls[0].url, "http://127.0.0.1:8080/api/vaults/my%20vault%2F%C3%BC/note?path=a.md");
  assert.equal(
    api.assetUrl("notes", "img/a b.png"),
    "http://127.0.0.1:8080/api/vaults/notes/raw?path=img%2Fa%20b.png",
  );
  // Without a base the URLs are of the same origin.
  assert.equal(
    setup(() => ok(note)).api.assetUrl("notes", "a.png"),
    "/api/vaults/notes/raw?path=a.png",
  );
});

test("save sends the text exactly as given: no trimming, no newline changes, never invalid UTF-8", async () => {
  const texts = [
    "",
    "﻿---\r\ntitle: Título\r\n---\r\n# Hola  \r\ntrailing  \r\nbare\rCR\r\n\r\n\r\n",
    "  leading and trailing spaces \n\n\n",
    "no final newline",
    "   separators JSON writers escape, and 😀",
    "a lone \uD800 high and a lone \uDC00 low surrogate",
  ];
  for (const content of texts) {
    const { api, calls } = setup(() => ok(note));
    await api.save("notes", "a.md", content, "r1");
    const body = calls[0].body!;
    assert.deepEqual(JSON.parse(body), { content, revision: "r1" }, JSON.stringify(content));
    // The server answers 400 to a body that is not UTF-8: what is sent must survive the encoding.
    assert.equal(
      new TextDecoder("utf-8", { fatal: true }).decode(new TextEncoder().encode(body)),
      body,
    );
  }
});

test("a save that lost is a ConflictError that names the revision on disk", async () => {
  const { api } = setup(() => stale("r9"));
  const error = await api.save("notes", "a.md", "mine", "r1").catch((caught: unknown) => caught);
  assert.ok(error instanceof ConflictError);
  assert.ok(error instanceof ApiError && error instanceof Error);
  assert.equal(error.currentRevision, "r9");
  assert.equal(error.status, 409);
  assert.equal(error.message, "note changed on disk");
  assert.equal(error.name, "ConflictError");
});

test("any other 409 is a plain ApiError: creating over a note, a name taken, a 409 with no revision", async () => {
  const exists = () => problem(409, "already exists");
  const cases: [string, () => Response, (api: ApiClient) => Promise<unknown>][] = [
    ["create", exists, (api) => api.create("notes", "a.md", "x")],
    ["mkdir", exists, (api) => api.mkdir("notes", "a")],
    ["move", exists, (api) => api.move("notes", "a.md", "b.md")],
    [
      "save with no errors",
      () => problem(409, "note changed on disk"),
      (api) => api.save("notes", "a.md", "x", "r1"),
    ],
    ["save with an empty revision", () => stale(""), (api) => api.save("notes", "a.md", "x", "r1")],
    [
      "save with a revision that is no string",
      () => stale(7),
      (api) => api.save("notes", "a.md", "x", "r1"),
    ],
  ];
  for (const [name, answer, run] of cases) {
    const { api } = setup(answer);
    const error = await run(api).catch((caught: unknown) => caught);
    assert.ok(error instanceof ApiError && !(error instanceof ConflictError), name);
    assert.equal(error.status, 409, name);
    assert.equal(error.name, "ApiError", name);
  }
});

test("files asks with the tag it was given, as it is, and answers notModified on a 304", async () => {
  const fresh = setup(() => ok(listing, { headers: { ETag: '"fp1"' } }));
  assert.deepEqual(await fresh.api.files("notes"), { notModified: false, listing, etag: '"fp1"' });
  assert.equal(fresh.calls[0].headers.has("If-None-Match"), false);

  const again = setup(() => new Response(null, { status: 304, headers: { ETag: '"fp1"' } }));
  assert.deepEqual(await again.api.files("notes", '"fp1"'), { notModified: true });
  assert.deepEqual(Object.fromEntries(again.calls[0].headers), { "if-none-match": '"fp1"' });

  // A listing that changed comes with its own tag.
  const changed = setup(() =>
    ok({ ...listing, fingerprint: "fp2" }, { headers: { ETag: '"fp2"' } }),
  );
  const answer = await changed.api.files("notes", '"fp1"');
  assert.deepEqual(answer, {
    notModified: false,
    listing: { ...listing, fingerprint: "fp2" },
    etag: '"fp2"',
  });
  assert.equal(changed.calls[0].headers.get("If-None-Match"), '"fp1"');

  // The tag is the fingerprint in quotes: that is what the server sends, and what it falls back to.
  const bare = setup(() => ok(listing));
  assert.deepEqual(await bare.api.files("notes"), { notModified: false, listing, etag: '"fp1"' });
});

test("revision is null when the note is gone, and any other failure throws", async () => {
  assert.equal(await setup(() => problem(404, "not found")).api.revision("notes", "a.md"), null);
  for (const [status, detail] of [
    [400, "invalid path"],
    [413, "note exceeds 2 MiB"],
    [415, "not a UTF-8 markdown note"],
    [500, "Could not access the vault. Try again."],
  ] as const) {
    const { api } = setup(() => problem(status, detail));
    await assert.rejects(api.revision("notes", "a.md"), {
      name: "ApiError",
      status,
      message: detail,
    });
  }
});

test("search sends the query encoded and the limit only when there is one, and returns the hits", async () => {
  const query = "¿Qué es? & x=y";
  const encoded = encodeURIComponent(query);
  for (const [limit, url] of [
    [undefined, `/api/vaults/notes/search?q=${encoded}`],
    [5, `/api/vaults/notes/search?q=${encoded}&limit=5`],
  ] as const) {
    const { api, calls } = setup(() => ok({ hits: [] }));
    assert.deepEqual(await api.search("notes", query, limit), []);
    assert.equal(calls[0].url, url);
  }
});

test("a failure is an ApiError with the status and the message the server gave", async () => {
  const long = "x".repeat(500);
  const cases: [string, () => Response, number, string][] = [
    ["a problem says why in its detail", () => problem(404, "not found"), 404, "not found"],
    [
      "a problem with no detail has its title",
      () => new Response('{"title":"Forbidden","status":403}', { status: 403 }),
      403,
      "Forbidden",
    ],
    [
      "a guard problem",
      () => problem(403, "X-Notebook header is required"),
      403,
      "X-Notebook header is required",
    ],
    // The Go mux answers an unknown route or method in plain text, not as a problem.
    [
      "a plain text answer is its text",
      () => new Response("404 page not found\n", { status: 404 }),
      404,
      "404 page not found",
    ],
    [
      "a plain 405",
      () => new Response("Method Not Allowed\n", { status: 405 }),
      405,
      "Method Not Allowed",
    ],
    [
      "an empty answer is its status text",
      () => new Response(null, { status: 502, statusText: "Bad Gateway" }),
      502,
      "Bad Gateway",
    ],
    [
      "an empty answer of a status with no text (HTTP/2)",
      () => new Response(null, { status: 502 }),
      502,
      "HTTP 502",
    ],
    [
      "JSON that is no problem is its text",
      () => new Response('{"error":"nope"}', { status: 500 }),
      500,
      '{"error":"nope"}',
    ],
    [
      "a page of a proxy is cut short",
      () => new Response(`<html>${long}</html>`, { status: 502 }),
      502,
      `<html>${long}</html>`.slice(0, 200) + "…",
    ],
  ];
  for (const [name, answer, status, message] of cases) {
    const { api } = setup(answer);
    await assert.rejects(api.note("notes", "a.md"), { name: "ApiError", status, message }, name);
  }
});

test("a request that gets no answer is an ApiError with status 0, never a bare TypeError", async () => {
  const cause = new TypeError("fetch failed");
  for (const { name, run } of operations) {
    const { api } = setup(() => {
      throw cause;
    });
    await assert.rejects(
      run(api),
      (error: unknown) => {
        assert.ok(error instanceof ApiError, name);
        assert.equal(error.status, 0, name);
        assert.equal(error.message, "Could not reach the server", name);
        assert.equal(error.cause, cause, name);
        return true;
      },
      name,
    );
  }
});

test("an answer that is not the JSON that was asked for is an ApiError, not a SyntaxError", async () => {
  // What a dev server or a proxy answers for a route it does not know: the page.
  const { api } = setup(
    () => new Response("<!doctype html><title>Notebook</title>", { status: 200 }),
  );
  await assert.rejects(api.note("notes", "a.md"), {
    name: "ApiError",
    status: 200,
    message: "Unexpected response from the server",
  });
});
