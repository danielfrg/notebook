/*
 * The typed client of the REST API (internal/httpapi): one function per
 * endpoint, over a fetch that is passed in, so that it runs in the browser and
 * under node:test alike and touches no global. Whatever the server or the
 * network does wrong is an ApiError: an answer that is no success has its
 * status and the server's own message, and a request that got no answer has the
 * status 0. Nothing is retried here, and note text is sent exactly as given.
 */

export type VaultInfo = { name: string };
export type FileEntry = { path: string; size: number; modified: string };
export type Listing = {
  notes: FileEntry[];
  assets: FileEntry[];
  folders: string[];
  fingerprint: string;
};
export type Note = {
  path: string;
  content: string;
  revision: string;
  size: number;
  modified: string;
};
export type SearchHit = {
  path: string;
  title: string;
  score: number;
  modified: string;
  snippets: { line: number; text: string }[];
};

/** A request that did not succeed. status is the HTTP status, or 0 when nothing answered. */
export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number, options?: ErrorOptions) {
    super(message, options);
    this.name = "ApiError";
    this.status = status;
  }
}

/** A save that lost against another writer: currentRevision is what is on disk now. */
export class ConflictError extends ApiError {
  currentRevision: string;

  constructor(message: string, currentRevision: string) {
    super(message, 409);
    this.name = "ConflictError";
    this.currentRevision = currentRevision;
  }
}

export interface ApiClient {
  vaults(): Promise<{ readOnly: boolean; vaults: VaultInfo[] }>;
  /**
   * The listing of a vault. Pass the etag of the listing you have, as it came
   * back (quoted), and an unchanged vault answers { notModified: true }.
   */
  files(
    vault: string,
    etag?: string,
  ): Promise<{ notModified: true } | { notModified: false; listing: Listing; etag: string }>;
  note(vault: string, path: string, signal?: AbortSignal): Promise<Note>;
  /** null when the note is gone (404); any other failure throws. */
  revision(
    vault: string,
    path: string,
    signal?: AbortSignal,
  ): Promise<{ revision: string; modified: string } | null>;
  /** Throws a ConflictError when the note changed on disk since `revision` was loaded. */
  save(vault: string, path: string, content: string, revision: string): Promise<Note>;
  create(vault: string, path: string, content?: string): Promise<Note>;
  mkdir(vault: string, path: string): Promise<void>;
  move(vault: string, from: string, to: string): Promise<{ path: string }>;
  remove(vault: string, path: string): Promise<void>;
  /** The best hits first. The server caps them at 50, and limit only asks for fewer. */
  search(vault: string, q: string, limit?: number): Promise<SearchHit[]>;
  /** The URL of an image of the vault, for an <img>. */
  assetUrl(vault: string, path: string): string;
}

type Options = {
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  /** Statuses outside 2xx that are answers and not failures. */
  allow?: number[];
  /** Whether a 409 that names the revision on disk is a ConflictError. */
  conflicts?: boolean;
};

// A message is for the UI: what a proxy answers in its own page is cut short.
const MAX_MESSAGE = 200;

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null;

const firstText = (...values: unknown[]): string | undefined =>
  values.find((value): value is string => typeof value === "string" && value.trim() !== "");

/**
 * The error of an answer that is no success. The server writes problem JSON
 * (detail says what is wrong; a stale save also has the revision on disk in
 * errors[0].value), except that its mux answers an unknown route or method in
 * plain text. So the message is the detail, then the title, then the text of
 * the answer, then the status text, which HTTP/2 does not have.
 */
async function failure(response: Response, conflicts: boolean): Promise<ApiError> {
  const text = await response.text().catch(() => "");
  let body: unknown;
  try {
    body = JSON.parse(text);
  } catch {
    // Plain text, or nothing.
  }
  const problem = isRecord(body) ? body : {};
  const message = (
    firstText(problem.detail, problem.title, text, response.statusText) ?? `HTTP ${response.status}`
  ).trim();
  const shortMessage = message.length > MAX_MESSAGE ? `${message.slice(0, MAX_MESSAGE)}…` : message;

  const first = Array.isArray(problem.errors) ? problem.errors[0] : undefined;
  const revision = isRecord(first) ? first.value : undefined;
  if (conflicts && response.status === 409 && typeof revision === "string" && revision !== "") {
    return new ConflictError(shortMessage, revision);
  }
  return new ApiError(shortMessage, response.status);
}

async function json<T>(response: Response): Promise<T> {
  try {
    return (await response.json()) as T;
  } catch (error) {
    // A page where the JSON should be, as a dev server or a proxy answers an unknown route.
    throw new ApiError("Unexpected response from the server", response.status, { cause: error });
  }
}

/**
 * fetchImpl is called as a plain function, never as a method (a browser's fetch
 * throws when it is called as one), and base goes in front of every /api URL as
 * it is, so it has no trailing slash. The default base is the page's own origin.
 */
export function createApi(fetchImpl: typeof fetch = fetch, base = ""): ApiClient {
  const vaultUrl = (vault: string, endpoint: string) =>
    `${base}/api/vaults/${encodeURIComponent(vault)}/${endpoint}`;
  // Paths go in the query, escaped as a component: a "%", "#", "+", "&" or "," in a name is part of
  // it, and an unescaped "+" is read back as a space.
  const entryUrl = (vault: string, endpoint: string, path: string) =>
    `${vaultUrl(vault, endpoint)}?path=${encodeURIComponent(path)}`;

  async function send(
    method: string,
    url: string,
    { body, headers, signal, allow = [], conflicts = false }: Options = {},
  ): Promise<Response> {
    const sent: Record<string, string> = { ...headers };
    // What the guard of the server asks of a request that changes something: the
    // header that a form cannot send, and the type of a body, which has to be JSON.
    if (method !== "GET") sent["X-Notebook"] = "1";
    if (body !== undefined) sent["Content-Type"] = "application/json";

    let response: Response;
    try {
      response = await fetchImpl(url, {
        method,
        headers: sent,
        signal,
        // JSON.stringify writes a lone surrogate as an escape, so the body is always valid UTF-8.
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch (error) {
      throw new ApiError("Could not reach the server", 0, { cause: error });
    }
    if (response.ok || allow.includes(response.status)) return response;
    throw await failure(response, conflicts);
  }

  return {
    vaults: async () =>
      json<{ readOnly: boolean; vaults: VaultInfo[] }>(await send("GET", `${base}/api/vaults`)),

    files: async (vault, etag) => {
      const response = await send("GET", vaultUrl(vault, "files"), {
        headers: etag ? { "If-None-Match": etag } : undefined,
        allow: [304],
      });
      if (response.status === 304) return { notModified: true };
      const listing = await json<Listing>(response);
      // The server's tag is the fingerprint in quotes.
      return {
        notModified: false,
        listing,
        etag: response.headers.get("ETag") ?? `"${listing.fingerprint}"`,
      };
    },

    note: async (vault, path, signal) =>
      json<Note>(await send("GET", entryUrl(vault, "note", path), { signal })),

    revision: async (vault, path, signal) => {
      const response = await send("GET", entryUrl(vault, "revision", path), {
        allow: [404],
        signal,
      });
      return response.status === 404
        ? null
        : json<{ revision: string; modified: string }>(response);
    },

    save: async (vault, path, content, revision) =>
      json<Note>(
        await send("PUT", entryUrl(vault, "note", path), {
          body: { content, revision },
          conflicts: true,
        }),
      ),

    create: async (vault, path, content) =>
      json<Note>(await send("POST", vaultUrl(vault, "notes"), { body: { path, content } })),

    mkdir: async (vault, path) => {
      await send("POST", vaultUrl(vault, "folders"), { body: { path } });
    },

    move: async (vault, from, to) =>
      json<{ path: string }>(await send("POST", vaultUrl(vault, "move"), { body: { from, to } })),

    remove: async (vault, path) => {
      await send("DELETE", entryUrl(vault, "entry", path));
    },

    search: async (vault, q, limit) => {
      const query =
        `q=${encodeURIComponent(q)}` +
        (limit === undefined ? "" : `&limit=${encodeURIComponent(String(limit))}`);
      const { hits } = await json<{ hits: SearchHit[] }>(
        await send("GET", `${vaultUrl(vault, "search")}?${query}`),
      );
      return hits;
    },

    assetUrl: (vault, path) => entryUrl(vault, "raw", path),
  };
}
