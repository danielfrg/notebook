import { ApiError, createApi, type ApiClient, type Listing, type VaultInfo } from "../api.ts";
import { noteFileName, renameTarget, validateNoteName } from "../entry-names.ts";
import { detectLineSeparator } from "../editor/linesep.ts";
import { joinPath, parentFolder, validateEntryName } from "../paths.ts";
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
  externalConflict = $state(false);
  missingNote = $state(false);
  navigationBlocked = $state(false);
  focusRequest = $state(0);
  lineSeparatorIssue: string | null = $state(null);
  bom = "";
  controller: SaveController | null = null;
  private api: ApiClient = createApi();
  private generation = 0;
  private started = false;
  private pendingPath: string | null = null;
  private pendingExit: { vault: string | null; path: string | null } | null = null;
  private noteAbort: AbortController | null = null;
  private notePollSequence = 0;
  private notePollInFlight: Promise<void> | null = null;
  private lastExternalRevision: string | null = null;
  private filesSequence = 0;
  private listingEtag: string | undefined;
  private saveCompletion = 0;
  private noteTimer: ReturnType<typeof setInterval> | undefined;
  private filesTimer: ReturnType<typeof setInterval> | undefined;
  private lastReturnRefresh = 0;
  private fileMutationPending = 0;

  get dirty(): boolean { return this.controller?.state !== "clean" && this.controller !== null; }

  async init(): Promise<void> {
    if (this.started) return;
    this.started = true;
    const generation = ++this.generation;
    this.mode = stored(MODE_KEY) === "source" ? "source" : "live";
    try {
      const result = await this.api.vaults();
      if (generation !== this.generation) return;
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
      if (!this.started) return;
      window.addEventListener("focus", this.handleFocus);
      if (typeof document !== "undefined") document.addEventListener("visibilitychange", this.handleVisibilityChange);
      this.noteTimer = setInterval(() => { if (this.visible()) void this.pollNote(); }, 5000);
      this.filesTimer = setInterval(() => { if (this.visible()) void this.refreshFiles().catch(() => {}); }, 15000);
    } catch (error) {
      if (generation !== this.generation) return;
      this.error = error instanceof Error ? error.message : "Could not load vaults";
      this.loading = false;
    }
  }

  dispose(): void {
    this.started = false;
    this.generation++;
    window.removeEventListener("hashchange", this.handleHashChange);
    window.removeEventListener("focus", this.handleFocus);
    if (typeof document !== "undefined") document.removeEventListener("visibilitychange", this.handleVisibilityChange);
    if (this.noteTimer !== undefined) clearInterval(this.noteTimer);
    if (this.filesTimer !== undefined) clearInterval(this.filesTimer);
    this.noteTimer = undefined;
    this.filesTimer = undefined;
    this.noteAbort?.abort();
    this.noteAbort = null;
    this.notePollInFlight = null;
    void this.controller?.flush();
  }

  async retry(): Promise<void> {
    if (this.vault) await this.loadVault(this.vault, parseRoute(location.hash).path);
    else { this.dispose(); await this.init(); }
  }

  selectVault(name: string): void {
    this.navigate(nextRoute({ vault: this.vault, path: this.path }, { type: "vault", name }, this.vaults.map((v) => v.name)));
  }

  openNote(path: string | null): void {
    this.navigate(nextRoute({ vault: this.vault, path: this.path }, { type: "note", path }, this.vaults.map((v) => v.name)));
  }

  assetUrl(path: string): string {
    return this.vault ? this.api.assetUrl(this.vault, path) : "";
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

  /** Refresh both resources on focus; intervals call each half separately. */
  async pollNow(): Promise<void> {
    if (!this.visible()) return;
    await Promise.all([this.pollNote(), this.refreshFiles().catch(() => {})]);
  }

  async createNote(folder: string, name: string): Promise<void> {
    this.requireWritable();
    await this.ensureSafeMutation();
    const reason = validateNoteName(name);
    if (reason) throw new Error(reason);
    const vault = this.requireVault();
    const path = joinPath(folder, noteFileName(name));
    await this.api.create(vault, path, "");
    await this.refreshAfterMutation();
    this.expandTo(path);
    this.navigate({ vault, path });
    await this.loadNote(path);
    this.focusRequest++;
  }

  async createFolder(parent: string, name: string): Promise<void> {
    this.requireWritable();
    name = name.trim();
    const reason = validateEntryName(name);
    if (reason) throw new Error(reason);
    const path = joinPath(parent, name);
    await this.api.mkdir(this.requireVault(), path);
    await this.refreshAfterMutation();
    this.expandTo(path);
    this.expanded = new Set([...this.expanded, path]);
  }

  async renameEntry(path: string, newName: string): Promise<void> {
    this.requireWritable();
    newName = newName.trim();
    const isFolder = this.listing?.folders.includes(path) ?? false;
    const reason = isFolder ? validateEntryName(newName) : null;
    if (reason) throw new Error(reason);
    const to = isFolder ? joinPath(parentFolder(path), newName) : renameTarget(path, newName);
    if (to === path) return;
    const vault = this.requireVault();
    const open = this.path === path || this.path?.startsWith(`${path}/`);
    const openPath = this.path;
    const openController = this.controller;
    this.beginFileMutation();
    try {
      if (open) await this.ensureSafeMutation();
      await this.api.move(vault, path, to);
      await this.refreshAfterMutation();
      if (this.vault !== vault) return;
      if (isFolder) {
        this.expanded = new Set([...this.expanded].map((entry) =>
          entry === path || entry.startsWith(`${path}/`) ? to + entry.slice(path.length) : entry));
      }
      if (open && openPath && this.path === openPath && this.controller === openController) {
        const next = to + openPath.slice(path.length);
        this.navigate({ vault, path: next });
        await this.loadNote(next);
      }
      this.expandTo(to);
    } finally {
      this.fileMutationPending--;
    }
  }

  async trashEntry(path: string): Promise<void> {
    this.requireWritable();
    const vault = this.requireVault();
    const open = this.path === path || this.path?.startsWith(`${path}/`);
    const openPath = this.path;
    const openController = this.controller;
    this.beginFileMutation();
    try {
      if (open) await this.ensureSafeMutation();
      await this.api.remove(vault, path);
      await this.refreshAfterMutation();
      if (this.vault === vault && open && this.path === openPath && this.controller === openController) {
        this.navigate({ vault, path: null });
        await this.loadNote(null);
      }
    } finally {
      this.fileMutationPending--;
    }
  }

  async reloadFromDisk(): Promise<void> {
    const { vault, path, controller, noteAbort } = this.openContext();
    if (controller.state === "saving") await controller.flush();
    try {
      const note = await this.api.note(vault, path, noteAbort.signal);
      if (!this.sameOpen(vault, path, controller, noteAbort)) return;
      controller.reload(note.content, note.revision);
      this.adoptContent(note.content);
      this.externalConflict = false;
      this.missingNote = false;
      this.navigationBlocked = false;
      this.pendingExit = null;
      this.lastExternalRevision = null;
    } catch (error) {
      if (this.sameOpen(vault, path, controller, noteAbort) && error instanceof ApiError && error.status === 404) this.markMissing();
      else if (!noteAbort.signal.aborted) throw error;
    }
  }

  async overwriteDisk(): Promise<void> {
    this.requireWritable();
    const { vault, path, controller, noteAbort } = this.openContext();
    if (controller.state === "saving") await controller.flush();
    const current = await this.api.revision(vault, path, noteAbort.signal);
    if (!this.sameOpen(vault, path, controller, noteAbort)) return;
    if (!current) { this.markMissing(); return; }
    await controller.overwrite(current.revision);
  }

  async saveMissingAsNew(name: string): Promise<void> {
    this.requireWritable();
    const reason = validateNoteName(name);
    if (reason) throw new Error(reason);
    const { vault, path, controller } = this.openContext();
    const newPath = joinPath(parentFolder(path), noteFileName(name));
    const note = await this.api.create(vault, newPath, controller.content);
    controller.reload(note.content, note.revision);
    await this.refreshAfterMutation();
    this.navigate({ vault, path: newPath });
    await this.loadNote(newPath);
    this.focusRequest++;
  }

  /** Called only after the user confirms that unsaved text may be lost. */
  discardLocalEdits(): void {
    if (!this.controller || !this.vault) return;
    const destination = this.pendingExit ?? { vault: this.vault, path: null };
    this.controller.reload(this.controller.content, this.controller.revision);
    this.externalConflict = false;
    this.missingNote = false;
    this.navigationBlocked = false;
    this.pendingExit = null;
    this.navigate(destination);
  }

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

  private refreshOnReturn(): void {
    if (!this.visible() || Date.now() - this.lastReturnRefresh < 250) return;
    this.lastReturnRefresh = Date.now();
    void this.pollNow();
  }

  private handleFocus = (): void => { this.refreshOnReturn(); };
  private handleVisibilityChange = (): void => { this.refreshOnReturn(); };

  private visible(): boolean {
    return typeof document === "undefined" || document.visibilityState === "visible";
  }

  private requireVault(): string {
    if (!this.vault) throw new Error("No vault is open");
    return this.vault;
  }

  private requireWritable(): void {
    if (this.readOnly) throw new Error("This vault is read-only");
  }

  private openContext(): { vault: string; path: string; controller: SaveController; noteAbort: AbortController } {
    if (!this.vault || !this.path || !this.controller || !this.noteAbort) throw new Error("No note is open");
    return { vault: this.vault, path: this.path, controller: this.controller, noteAbort: this.noteAbort };
  }

  private sameOpen(vault: string, path: string, controller: SaveController, abort: AbortController): boolean {
    return !abort.signal.aborted && this.vault === vault && this.path === path && this.controller === controller && this.noteAbort === abort;
  }

  private expandTo(path: string): void {
    this.expanded = new Set([...this.expanded, ...ancestors(path)]);
  }

  private async ensureSafeMutation(): Promise<void> {
    if (!this.controller) return;
    await this.controller.flush();
    if (this.controller.state !== "clean" || this.externalConflict || this.missingNote) {
      throw new Error("Resolve the open note before changing files");
    }
  }

  private adoptContent(content: string): void {
    const { bom, value } = splitBom(content);
    this.bom = bom;
    this.value = value;
    this.lineSeparatorIssue = readOnlyReason({ lineSeparator: detectLineSeparator(value) });
  }

  private markMissing(): void {
    this.missingNote = true;
    this.externalConflict = false;
  }

  private beginFileMutation(): void {
    this.fileMutationPending++;
    this.notePollSequence++;
    this.notePollInFlight = null;
  }

  private pollNote(): Promise<void> {
    if (this.notePollInFlight) return this.notePollInFlight;
    const running = this.pollNoteOnce();
    this.notePollInFlight = running;
    void running.finally(() => {
      if (this.notePollInFlight === running) this.notePollInFlight = null;
    });
    return running;
  }

  private async pollNoteOnce(): Promise<void> {
    if (!this.visible() || this.fileMutationPending > 0 || !this.vault || !this.path || !this.controller || !this.noteAbort || this.controller.state === "saving") return;
    const { vault, path, controller, noteAbort } = this.openContext();
    const sequence = ++this.notePollSequence;
    const saveCompletion = this.saveCompletion;
    const current = () => this.sameOpen(vault, path, controller, noteAbort) &&
      sequence === this.notePollSequence && saveCompletion === this.saveCompletion;
    const saving = () => controller.state === "saving";
    try {
      const revision = await this.api.revision(vault, path, noteAbort.signal);
      if (!current() || saving()) return;
      if (!revision) { this.markMissing(); return; }
      this.missingNote = false;
      // This is the usual result of our own save; external() must not run for it.
      if (revision.revision === controller.revision) {
        this.externalConflict = false;
        this.lastExternalRevision = null;
        controller.reconcileRevision();
        return;
      }
      if (revision.revision === this.lastExternalRevision) return;
      const note = await this.api.note(vault, path, noteAbort.signal);
      if (!current() || saving()) return;
      if (note.revision === controller.revision) {
        this.missingNote = false;
        this.externalConflict = false;
        this.lastExternalRevision = null;
        controller.reconcileRevision();
        return;
      }
      if (controller.external(note.content, note.revision) === "applied") {
        this.adoptContent(note.content);
        this.externalConflict = false;
        this.missingNote = false;
      } else {
        this.externalConflict = true;
        this.lastExternalRevision = note.revision;
      }
    } catch (error) {
      if (!current()) return;
      if (error instanceof ApiError && error.status === 404) this.markMissing();
      // Transient network errors leave the editor and its save status alone.
    }
  }

  private async refreshFiles(force = false): Promise<void> {
    if (!this.vault) return;
    const vault = this.vault;
    const sequence = ++this.filesSequence;
    const result = await this.api.files(vault, force ? undefined : this.listingEtag);
    if (sequence !== this.filesSequence || vault !== this.vault) return;
    if (result.notModified) return;
    this.listingEtag = result.etag;
    this.listing = result.listing;
    this.tree = buildTree(result.listing);
  }

  private async refreshAfterMutation(): Promise<void> {
    // Once the write succeeds, a failed listing must not leave the dialog open
    // inviting the user to repeat an operation that already happened.
    await this.refreshFiles(true).catch(() => {});
  }

  private async followRoute(vault: string | null, path: string | null): Promise<void> {
    if (this.loading && vault !== null && this.vault === vault) {
      this.pendingPath = path;
      return;
    }
    if (this.vault === vault && this.path === path && !this.error) return;
    if (this.controller && this.controller.state !== "clean") await this.controller.flush();
    if (this.controller && this.controller.state !== "clean") {
      this.navigationBlocked = true;
      this.pendingExit = { vault, path };
      if (this.vault) location.replace(formatRoute(this.vault, this.path));
      return;
    }
    this.navigationBlocked = false;
    this.pendingExit = null;
    if (vault !== this.vault) await this.loadVault(vault, path);
    else await this.loadNote(path);
  }

  private async loadVault(vault: string | null, path: string | null): Promise<void> {
    const generation = ++this.generation;
    this.pendingPath = path;
    this.vault = vault;
    this.path = null;
    this.listing = null;
    this.listingEtag = undefined;
    this.filesSequence++;
    this.tree = [];
    this.expanded = new Set();
    this.controller = null;
    this.noteAbort?.abort();
    this.noteAbort = null;
    this.notePollInFlight = null;
    this.lastExternalRevision = null;
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
        this.listingEtag = result.etag;
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
    this.noteAbort?.abort();
    this.noteAbort = path ? new AbortController() : null;
    this.notePollSequence++;
    this.notePollInFlight = null;
    this.lastExternalRevision = null;
    this.path = path;
    this.value = "";
    this.bom = "";
    this.controller = null;
    this.saveState = "clean";
    this.saveDetail = null;
    this.externalConflict = false;
    this.missingNote = false;
    this.navigationBlocked = false;
    this.pendingExit = null;
    this.noteError = null;
    this.lineSeparatorIssue = null;
    if (!path || !this.vault) { this.noteLoading = false; return; }
    this.expandTo(path);
    this.noteLoading = true;
    const vault = this.vault;
    try {
      const note = await this.api.note(vault, path, this.noteAbort?.signal);
      if (generation !== this.generation) return;
      this.adoptContent(note.content);
      const controller = new SaveController({
        debounceMs: 1000,
        save: async (content, revision) => {
          const result = await this.api.save(vault, path, content, revision);
          this.saveCompletion++;
          if (this.controller === controller) {
            this.missingNote = false;
            this.externalConflict = false;
            this.navigationBlocked = false;
            this.pendingExit = null;
            this.lastExternalRevision = null;
          }
          return result;
        },
        onstate: (state, detail) => {
          if (this.controller !== controller) return;
          this.saveState = state;
          this.saveDetail = detail ?? null;
          if (state === "clean") this.externalConflict = false;
          if (state === "conflict") this.externalConflict = true;
          if (state === "error" && detail === "missing") this.markMissing();
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
