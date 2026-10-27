/*
 * The autosave of the open note: when the text in the editor is written to the
 * server, and what happens when the server refuses it. Plain TypeScript with no
 * DOM and no framework, so it runs under node:test with fake timers.
 *
 * A failed save is never tried again by the controller itself, so there is no
 * loop. After an error the next edit or flush saves again; after a conflict only
 * overwrite() or reload() moves on.
 */

/**
 * clean: the text is what the disk has. dirty: it is not, and a save is due a
 * debounce after the last edit. saving: a save is out. conflict: the save lost
 * against another writer, and the user has to say which text wins. error: the
 * save failed, and the text is not on disk.
 */
export type SaveState = "clean" | "dirty" | "saving" | "conflict" | "error";

export type SaveControllerOptions = {
  /** Quiet time after the last edit before a save is made. */
  debounceMs: number;
  /** Writes the text over the revision it was loaded at. May reject with a ConflictError. */
  save(content: string, revision: string): Promise<{ revision: string }>;
  /**
   * Called after every change of state, in order, from a microtask and never from
   * inside a method of the controller (edit() runs in the editor's update
   * listener, where the page must not be touched yet), so it is safe for it to do
   * anything. state is what changed to: the property of the controller may be
   * further on by then. detail is, for conflict, the revision on disk when the
   * server said which (pass it to overwrite()); for error, "missing" when the
   * note is gone and else the message of the error.
   */
  onstate(state: SaveState, detail?: string): void;
  /** Injectable for tests; the defaults are setTimeout and clearTimeout. */
  setTimer?: typeof setTimeout;
  clearTimer?: typeof clearTimeout;
};

type Timer = ReturnType<typeof setTimeout>;

/** What a rejected save may carry, by duck typing: this module does not know api.ts. */
type Failure = { status?: unknown; currentRevision?: unknown; message?: unknown };

export class SaveController {
  #options: SaveControllerOptions;
  #schedule: (run: () => void, ms: number) => Timer;
  #unschedule: (timer: Timer) => void;
  #state: SaveState = "clean";
  #content: string;
  #revision: string;
  /** The text of the version that #revision names: what the disk has, as far as is known. */
  #saved: string;
  /** The debounce, pending only while the state is dirty. */
  #timer: Timer | undefined;
  /** The save loop that is out (or was the last one). */
  #running: Promise<void> = Promise.resolve();
  /** Bumped by reload(): a save that was out then has no say any more. */
  #epoch = 0;

  constructor(options: SaveControllerOptions, initial: { content: string; revision: string }) {
    this.#options = options;
    const setTimer = options.setTimer ?? setTimeout;
    const clearTimer = options.clearTimer ?? clearTimeout;
    // Called as plain functions: a browser's setTimeout throws when it is called as a method.
    this.#schedule = (run, ms) => setTimer(run, ms);
    this.#unschedule = (timer) => clearTimer(timer);
    this.#content = initial.content;
    this.#revision = initial.revision;
    this.#saved = initial.content;
  }

  get state(): SaveState {
    return this.#state;
  }

  /** The text of the editor, as last edited or adopted. */
  get content(): string {
    return this.#content;
  }

  /** The revision that content is based on: it moves only on a successful save or an adoption. */
  get revision(): string {
    return this.#revision;
  }

  /**
   * The text of the editor changed. It becomes dirty and a save is due a debounce
   * from now, unless the text is what the disk has. In a save that is out the
   * text is only noted: the loop sends it as one follow-up when the save is back.
   * In a conflict it is only noted too: only the user can resolve it.
   */
  edit(content: string): void {
    this.#content = content;
    if (this.#state === "saving" || this.#state === "conflict") return;
    this.#cancel();
    if (content === this.#saved) {
      this.#set("clean");
      return;
    }
    this.#set("dirty");
    this.#timer = this.#schedule(() => {
      this.#timer = undefined;
      void this.#start(this.#revision);
    }, this.#options.debounceMs);
  }

  /**
   * Saves now what is unsaved (also after an error: that is how a retry is
   * asked for) and resolves when that save, and the follow-up for what was typed
   * meanwhile, are done. During a save that is out it waits for that one. It
   * resolves and never rejects: a failure is the state. In a conflict it does
   * nothing, for a save over the revision that lost would lose again.
   */
  async flush(): Promise<void> {
    if (this.#state === "saving") return this.#running;
    if (this.#state === "conflict" || this.#content === this.#saved) return;
    return this.#start(this.#revision);
  }

  /**
   * The file changed on disk, and this is its text and revision now. If nothing
   * is unsaved they are adopted, silently: the caller shows the text. Otherwise
   * (unsaved text, a save that is out, a save that failed) the answer is
   * "conflict" and nothing here changes, neither the text nor the revision nor
   * the state: it is for the caller to offer reload() and overwrite(). A save that
   * still goes out over the old revision is refused by the server, and that is
   * what moves the state to conflict. The caller should not report a revision
   * that the controller has already: that is the effect of its own save.
   */
  external(content: string, revision: string): "applied" | "conflict" {
    if (this.#state !== "clean") return "conflict";
    this.#content = content;
    this.#revision = revision;
    this.#saved = content;
    return "applied";
  }

  /** Adopts the version on disk and drops what is unsaved: it is clean, and no save is due. */
  reload(content: string, revision: string): void {
    this.#cancel();
    this.#epoch++;
    this.#content = content;
    this.#revision = revision;
    this.#saved = content;
    this.#set("clean");
  }

  /** The disk returned to our base revision; keep local text and resume autosave. */
  reconcileRevision(): void {
    if (this.#state !== "conflict") return;
    this.#set("dirty");
    this.edit(this.#content);
  }

  /**
   * Saves the current text over currentRevision, the revision that is on disk now,
   * whatever the state: the text wins. It saves even text that is saved already,
   * for the disk may have changed behind that save. A save that is out goes
   * first, so that two are never out at once. Ends clean, or in a conflict again
   * if the disk moved on once more. Never rejects.
   */
  async overwrite(currentRevision: string): Promise<void> {
    while (this.#state === "saving") await this.#running;
    return this.#start(currentRevision);
  }

  /** Starts a save loop over base. The caller has seen that none is out. */
  #start(base: string): Promise<void> {
    this.#cancel();
    this.#running = this.#drain(base);
    return this.#running;
  }

  /**
   * Saves the text, and what was typed while that save was out, until the disk
   * has the text of the editor. It does not reject: the way out of a failure is
   * the state. A reload() meanwhile makes it forget everything it was told.
   */
  async #drain(base: string): Promise<void> {
    const epoch = this.#epoch;
    let revision = base;
    do {
      const content = this.#content;
      this.#set("saving");
      try {
        ({ revision } = await this.#options.save(content, revision));
      } catch (error) {
        if (epoch === this.#epoch) this.#fail(error);
        return;
      }
      if (epoch !== this.#epoch) return;
      this.#revision = revision;
      this.#saved = content;
    } while (this.#content !== this.#saved);
    this.#set("clean");
  }

  /** The state after a failed save, by what the error says. Nothing is tried again. */
  #fail(error: unknown): void {
    const failure = (error ?? {}) as Failure;
    if (failure.status === 409) {
      const current = failure.currentRevision;
      this.#set("conflict", typeof current === "string" ? current : undefined);
    } else if (failure.status === 404) {
      // The note was deleted since it was loaded, and a save never makes it again.
      this.#set("error", "missing");
    } else {
      const { message } = failure;
      this.#set("error", typeof message === "string" && message !== "" ? message : undefined);
    }
  }

  #set(state: SaveState, detail?: string): void {
    if (state === this.#state) return;
    this.#state = state;
    queueMicrotask(() => this.#options.onstate(state, detail));
  }

  #cancel(): void {
    if (this.#timer === undefined) return;
    this.#unschedule(this.#timer);
    this.#timer = undefined;
  }
}
