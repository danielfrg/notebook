import { createApi, type ApiClient, type Listing, type VaultInfo } from "../api.ts";
import { detectLineSeparator } from "../editor/linesep.ts";
import { SaveController, type SaveState } from "../save-controller.ts";
import { formatRoute, parseRoute } from "../router.ts";
import { ancestors, buildTree, type TreeNode } from "../tree.ts";
import { joinBom, nextRoute, readOnlyReason, splitBom } from "./route-sync.ts";

const VAULT_KEY = "notebook-vault";
const MODE_KEY = "notebook-mode";

function stored(key: string): string | null {
  try { return localStorage.getItem(key); } catch { return null; }
}
function remember(key: string, value: string): void {
  try { localStorage.setItem(key, value); } catch { /* Storage may be disabled. */ }
}

class Workspace {
  vaults: VaultInfo[] = $state([]);
  readOnly = $state(false);
  vault: string | null = $state(null);
  listing: Listing | null = $state(null);
  tree: TreeNode[] = $state([]);
  expanded: Set<string> = $state(new Set());
  path: string | null = $state(null);
  value = $state("");
  loading = $state(true);
  noteLoading = $state(false);
  error: string | null = $state(null);
  noteError: string | null = $state(null);
  mode: "live" | "source" = $state("live");
  saveState: SaveState = $state("clean");
  saveDetail: string | null = $state(null);
  lineSeparatorIssue: string | null = $state(null);
  bom = "";
  controller: SaveController | null = null;
  private api: ApiClient = createApi();
  private generation = 0;
  private started = false;
  private pendingPath: string | null = null;

  get dirty(): boolean { return this.controller?.state !== "clean" && this.controller !== null; }

  async init(): Promise<void> {
    if (this.started) return;
    this.started = true;
    this.mode = stored(MODE_KEY) === "source" ? "source" : "live";
    try {
      const result = await this.api.vaults();
      this.vaults = result.vaults;
      this.readOnly = result.readOnly;
      window.addEventListener("hashchange", this.handleHashChange);
      const parsed = parseRoute(location.hash);
      const preferred = parsed.vault ?? stored(VAULT_KEY);
      const route = nextRoute({ vault: preferred, path: parsed.path }, { type: "note", path: parsed.path }, result.vaults.map((v) => v.name));
      if (route.vault && location.hash !== formatRoute(route.vault, route.path)) {
        location.replace(formatRoute(route.vault, route.path));
      }
      await this.followRoute(route.vault, route.path);
    } catch (error) {
      this.error = error instanceof Error ? error.message : "Could not load vaults";
      this.loading = false;
    }
  }

  dispose(): void {
    window.removeEventListener("hashchange", this.handleHashChange);
    void this.controller?.flush();
  }

  async retry(): Promise<void> {
    if (this.vault) await this.loadVault(this.vault, parseRoute(location.hash).path);
    else { this.started = false; await this.init(); }
  }

  selectVault(name: string): void {
    this.navigate(nextRoute({ vault: this.vault, path: this.path }, { type: "vault", name }, this.vaults.map((v) => v.name)));
  }

  openNote(path: string | null): void {
    this.navigate(nextRoute({ vault: this.vault, path: this.path }, { type: "note", path }, this.vaults.map((v) => v.name)));
  }

  setMode(mode: "live" | "source"): void {
    this.mode = mode;
    remember(MODE_KEY, mode);
  }

  toggleFolder(path: string): void {
    const next = new Set(this.expanded);
    if (next.has(path)) next.delete(path); else next.add(path);
    this.expanded = next;
  }

  saveNow(): Promise<void> { return this.controller?.flush() ?? Promise.resolve(); }

  edit(value: string): void {
    // Feed Editor's update back synchronously. A stale prop would replace a newer edit.
    this.value = value;
    this.controller?.edit(joinBom(this.bom, value));
  }

  setLineSeparator(separator: "\n" | "\r\n" | "mixed"): void {
    this.lineSeparatorIssue = readOnlyReason({ lineSeparator: separator });
  }

  private navigate(route: { vault: string | null; path: string | null }): void {
    if (!route.vault) return;
    const hash = formatRoute(route.vault, route.path);
    if (location.hash !== hash) location.hash = hash;
  }

  private handleHashChange = (): void => {
    const parsed = parseRoute(location.hash);
    const route = nextRoute(parsed, { type: "note", path: parsed.path }, this.vaults.map((v) => v.name));
    if (route.vault && location.hash !== formatRoute(route.vault, route.path)) {
      location.replace(formatRoute(route.vault, route.path));
    }
    void this.followRoute(route.vault, route.path);
  };

  private async followRoute(vault: string | null, path: string | null): Promise<void> {
    if (this.loading && vault !== null && this.vault === vault) {
      this.pendingPath = path;
      return;
    }
    if (this.vault === vault && this.path === path && !this.error) return;
    if (this.controller && this.controller.state !== "clean") await this.controller.flush();
    if (this.controller && this.controller.state !== "clean") {
      if (this.vault) location.replace(formatRoute(this.vault, this.path));
      return;
    }
    if (vault !== this.vault) await this.loadVault(vault, path);
    else await this.loadNote(path);
  }

  private async loadVault(vault: string | null, path: string | null): Promise<void> {
    const generation = ++this.generation;
    this.pendingPath = path;
    this.vault = vault;
    this.path = null;
    this.listing = null;
    this.tree = [];
    this.expanded = new Set();
    this.controller = null;
    this.value = "";
    this.noteError = null;
    this.error = null;
    this.loading = true;
    if (!vault) { this.loading = false; return; }
    remember(VAULT_KEY, vault);
    try {
      const result = await this.api.files(vault);
      if (generation !== this.generation) return;
      if (!result.notModified) {
        this.listing = result.listing;
        this.tree = buildTree(result.listing);
      }
      this.loading = false;
      await this.loadNote(this.pendingPath);
    } catch (error) {
      if (generation !== this.generation) return;
      this.error = error instanceof Error ? error.message : "Could not load vault";
      this.loading = false;
    }
  }

  private async loadNote(path: string | null): Promise<void> {
    const generation = ++this.generation;
    this.path = path;
    this.value = "";
    this.bom = "";
    this.controller = null;
    this.saveState = "clean";
    this.saveDetail = null;
    this.noteError = null;
    this.lineSeparatorIssue = null;
    if (!path || !this.vault) { this.noteLoading = false; return; }
    this.expanded = new Set([...this.expanded, ...ancestors(path)]);
    this.noteLoading = true;
    const vault = this.vault;
    try {
      const note = await this.api.note(vault, path);
      if (generation !== this.generation) return;
      const { bom, value } = splitBom(note.content);
      this.bom = bom;
      this.value = value;
      this.lineSeparatorIssue = readOnlyReason({ lineSeparator: detectLineSeparator(value) });
      const controller = new SaveController({
        debounceMs: 1000,
        save: (content, revision) => this.api.save(vault, path, content, revision),
        onstate: (state, detail) => {
          if (this.controller !== controller) return;
          this.saveState = state;
          this.saveDetail = detail ?? null;
        },
      }, { content: note.content, revision: note.revision });
      this.controller = controller;
    } catch (error) {
      if (generation !== this.generation) return;
      this.noteError = readOnlyReason({ error }) ?? (error instanceof Error ? error.message : "Could not open note");
    } finally {
      if (generation === this.generation) this.noteLoading = false;
    }
  }
}

export const workspace = new Workspace();
