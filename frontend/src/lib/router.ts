/*
 * Hash routes: #/<vault>/<seg1>/<seg2>..., each segment percent-encoded on its
 * own so that a name with a "?", "#", "%" or a space stays one segment and one
 * piece of the hash. Parsed vaults and path parts are checked against the same
 * user-facing name rules before they can reach the API. Browser history is the back stack.
 */

import { validateEntryName } from "./paths.ts";

export type Route = { vault: string | null; path: string | null };

/** "#/notes/a/b%20c.md" for ("notes", "a/b c.md"); just the vault when there is no path. */
export function formatRoute(vault: string, path?: string | null): string {
  const segments = path ? path.split("/") : [];
  return "#/" + [vault, ...segments].map(encodeURIComponent).join("/");
}

/**
 * The vault and the path in a location hash, with or without its "#". Anything
 * that is not a route gives nulls. A malformed percent escape does not throw:
 * in the vault it makes the whole route null, in the path just the path (the
 * vault stays). Segments are cut before they are decoded, so an escaped slash
 * is not a separator, and an empty segment ("a//b", a trailing slash) makes the
 * path null, as the server refuses such a path rather than clean it.
 */
export function parseRoute(hash: string): Route {
  const rest = hash.startsWith("#") ? hash.slice(1) : hash;
  if (!rest.startsWith("/")) return { vault: null, path: null };

  const [first, ...segments] = rest.slice(1).split("/");
  const vault = decode(first);
  if (!vault || !/^[a-z0-9][a-z0-9_-]{0,31}$/.test(vault)) return { vault: null, path: null };
  // "#/notes" and "#/notes/" name the vault alone.
  if (segments.length === 0 || (segments.length === 1 && segments[0] === "")) {
    return { vault, path: null };
  }
  const parts = segments.map(decode);
  if (
    new TextEncoder().encode(parts.join("/")).length > 512 ||
    parts.some((part) => !part || validateEntryName(part) !== null)
  )
    return { vault, path: null };
  return { vault, path: parts.join("/") };
}

function decode(segment: string): string | null {
  try {
    return decodeURIComponent(segment);
  } catch {
    return null;
  }
}
