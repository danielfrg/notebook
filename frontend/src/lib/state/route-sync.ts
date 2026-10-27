import { ApiError } from "../api.ts";
import { validateEntryName } from "../paths.ts";
import type { Route } from "../router.ts";

export const validVault = (name: string): boolean => /^[a-z0-9][a-z0-9_-]{0,31}$/.test(name);
export const validPath = (path: string): boolean =>
  path !== "" && new TextEncoder().encode(path).length <= 512 && path.split("/").every((part) => validateEntryName(part) === null);

export function nextRoute(
  current: Route,
  action: { type: "vault"; name: string } | { type: "note"; path: string | null },
  vaults: string[],
): Route {
  const available = vaults.filter(validVault);
  const fallback = available[0] ?? null;
  const vault = current.vault && validVault(current.vault) && available.includes(current.vault)
    ? current.vault : fallback;
  if (action.type === "vault") {
    return { vault: validVault(action.name) && available.includes(action.name) ? action.name : vault, path: null };
  }
  if (action.path !== null && !validPath(action.path)) {
    return { vault, path: vault === current.vault ? current.path : null };
  }
  return { vault, path: vault === current.vault ? action.path : null };
}

export function readOnlyReason(input: { error?: unknown; lineSeparator?: "\n" | "\r\n" | "mixed" }): string | null {
  if (input.lineSeparator === "mixed") return "Mixed or bare-CR line endings; this note is read-only.";
  if (input.error instanceof ApiError && input.error.status === 413) return "This note is too large to open.";
  if (input.error instanceof ApiError && input.error.status === 415) return "This note is not UTF-8 text.";
  return null;
}

export function splitBom(content: string): { bom: string; value: string } {
  return content.startsWith("\ufeff") ? { bom: "\ufeff", value: content.slice(1) } : { bom: "", value: content };
}

export function joinBom(bom: string, value: string): string {
  return bom + value;
}
