import test from "node:test";
import assert from "node:assert/strict";
import { setImmediate as nextMacrotask } from "node:timers/promises";
import { ConflictError } from "./api.ts";
import { SaveController } from "./save-controller.ts";

/** Lets every promise continuation and queued notification run (the fake timers are not involved). */
const settle = () => nextMacrotask();

/** Timers that go off only when advance() says that time has passed, in the order they are due. */
function fakeTimers() {
  let now = 0;
  let nextId = 1;
  const pending = new Map<number, { at: number; run: () => void }>();
  return {
    // The real types carry Node's promisify marker: a stand-in only has to be callable.
    setTimer: ((run: () => void, ms: number) => {
      const id = nextId++;
      pending.set(id, { at: now + ms, run });
      return id;
    }) as unknown as typeof setTimeout,
    clearTimer: ((id: number) => {
      pending.delete(id);
    }) as unknown as typeof clearTimeout,
    get pending() {
      return pending.size;
    },
    advance(ms: number) {
      const end = now + ms;
      for (;;) {
        const due = [...pending]
          .filter(([, timer]) => timer.at <= end)
          .sort((a, b) => a[1].at - b[1].at)[0];
        if (!due) break;
        pending.delete(due[0]);
        now = due[1].at;
        due[1].run();
      }
      now = end;
    },
  };
}

type Save = {
  content: string;
  revision: string;
  resolve(revision: string): void;
  reject(error: unknown): void;
};

/**
 * A controller on "a" at revision r1, with a debounce of 1000ms. Every save it makes
 * waits in saves until the test resolves or rejects it; states is what onstate was told.
 */
function setup() {
  const timers = fakeTimers();
  const saves: Save[] = [];
  const states: string[] = [];
  const controller = new SaveController(
    {
      debounceMs: 1000,
      save: (content, revision) =>
        new Promise((resolve, reject) => {
          saves.push({ content, revision, resolve: (next) => resolve({ revision: next }), reject });
        }),
      onstate: (state, detail) => states.push(detail === undefined ? state : `${state}:${detail}`),
      setTimer: timers.setTimer,
      clearTimer: timers.clearTimer,
    },
    { content: "a", revision: "r1" },
  );
  return { controller, timers, saves, states };
}
type Harness = ReturnType<typeof setup>;

/** What each save was asked to write, and over which revision. */
const sent = ({ saves }: Harness) => saves.map(({ content, revision }) => [content, revision]);
const snapshot = ({ controller }: Harness) => [
  controller.state,
  controller.content,
  controller.revision,
];

/** Edits and lets the debounce pass: the save is out and waits for an answer. */
function startSave(h: Harness, content = "b") {
  h.controller.edit(content);
  h.timers.advance(1000);
}

const conflicted = (revision = "r9") => new ConflictError("note changed on disk", revision);

/** The first save lost against a write that made revision r9. */
async function loseSave(h: Harness) {
  startSave(h);
  h.saves[0].reject(conflicted());
  await settle();
}

test("rapid edits make one save, a debounce after the last one, with the latest text", async () => {
  const h = setup();
  h.controller.edit("ab");
  h.timers.advance(900);
  h.controller.edit("abc");
  // 1800ms after the first edit, when its own timer would have gone off long ago.
  h.timers.advance(900);
  assert.equal(h.saves.length, 0);
  assert.equal(h.controller.state, "dirty");

  h.timers.advance(100);
  assert.deepEqual(sent(h), [["abc", "r1"]]);
  assert.equal(h.controller.state, "saving");

  h.saves[0].resolve("r2");
  await settle();
  assert.deepEqual(snapshot(h), ["clean", "abc", "r2"]);
  assert.deepEqual(h.states, ["dirty", "saving", "clean"]);
  h.timers.advance(60_000);
  assert.equal(h.saves.length, 1);
});

test("text equal to the last saved text is never saved, and clean stays clean", async () => {
  const h = setup();
  h.controller.edit("a"); // what is on disk already
  assert.equal(h.controller.state, "clean");
  assert.equal(h.timers.pending, 0);

  h.controller.edit("ab");
  h.controller.edit("a"); // typed and deleted again before the debounce
  assert.equal(h.controller.state, "clean");
  assert.equal(h.timers.pending, 0);
  await h.controller.flush();
  h.timers.advance(60_000);
  assert.equal(h.saves.length, 0);

  // After a save the text to compare with is the one that was saved.
  startSave(h, "b");
  h.saves[0].resolve("r2");
  await settle();
  h.controller.edit("b");
  await h.controller.flush();
  assert.equal(h.saves.length, 1);
  assert.deepEqual(h.states, ["dirty", "clean", "dirty", "saving", "clean"]);
});

test("an edit while a save is out makes one follow-up save, with the new revision and the latest text", async () => {
  const h = setup();
  startSave(h);
  h.controller.edit("bc");
  h.controller.edit("bcd");
  assert.equal(h.controller.state, "saving");
  assert.equal(h.saves.length, 1);

  h.saves[0].resolve("r2");
  await settle();
  assert.deepEqual(sent(h), [
    ["b", "r1"],
    ["bcd", "r2"],
  ]);
  assert.equal(h.controller.state, "saving");

  h.saves[1].resolve("r3");
  await settle();
  assert.deepEqual(snapshot(h), ["clean", "bcd", "r3"]);
  h.timers.advance(60_000);
  assert.equal(h.saves.length, 2);
  // One run: the state does not flicker to dirty and clean between the two saves.
  assert.deepEqual(h.states, ["dirty", "saving", "clean"]);
});

test("typing back to the text that is being saved needs no follow-up", async () => {
  const h = setup();
  startSave(h, "b");
  h.controller.edit("bc");
  h.controller.edit("b");
  h.saves[0].resolve("r2");
  await settle();
  assert.deepEqual(snapshot(h), ["clean", "b", "r2"]);
  assert.equal(h.saves.length, 1);
});

test("flush saves at once, and resolves when the save and the follow-up are done", async () => {
  const h = setup();
  h.controller.edit("b");
  let flushed = false;
  const flushing = h.controller.flush().then(() => {
    flushed = true;
  });
  await settle();
  // The debounce is not waited for, and cannot make a second save later.
  assert.deepEqual(sent(h), [["b", "r1"]]);
  assert.equal(h.timers.pending, 0);

  h.controller.edit("bc"); // typed while the save is out
  h.saves[0].resolve("r2");
  await settle();
  assert.equal(flushed, false); // the follow-up is still out
  assert.deepEqual(sent(h), [
    ["b", "r1"],
    ["bc", "r2"],
  ]);

  h.saves[1].resolve("r3");
  await flushing;
  assert.equal(flushed, true);
  assert.deepEqual(snapshot(h), ["clean", "bc", "r3"]);
  h.timers.advance(60_000);
  assert.equal(h.saves.length, 2);
});

test("flush while a save is out waits for it, and rejects nothing: errors become state", async () => {
  const h = setup();
  startSave(h);
  let flushed = false;
  const flushing = h.controller.flush().then(() => {
    flushed = true;
  });
  await settle();
  assert.equal(h.saves.length, 1); // it joins the save that is out
  assert.equal(flushed, false); // and waits for it
  h.saves[0].reject(new Error("boom"));
  await flushing; // does not throw
  assert.equal(flushed, true);
  assert.deepEqual(snapshot(h), ["error", "b", "r1"]);
});

test("a save that lost is a conflict that waits for the user: no retry, no loop, nothing armed", async () => {
  const h = setup();
  await loseSave(h);
  assert.deepEqual(snapshot(h), ["conflict", "b", "r1"]);
  assert.deepEqual(h.states, ["dirty", "saving", "conflict:r9"]); // the revision on disk comes with it

  h.timers.advance(60_000);
  await h.controller.flush();
  assert.equal(h.saves.length, 1);

  // What is typed meanwhile is kept, and is for the user to resolve.
  h.controller.edit("bc");
  assert.equal(h.timers.pending, 0);
  h.timers.advance(60_000);
  await h.controller.flush();
  assert.deepEqual(snapshot(h), ["conflict", "bc", "r1"]);
  assert.equal(h.saves.length, 1);
});

test("a rejected save is an error, and is not retried by the controller", async () => {
  const withStatus = (status: number, message: string) =>
    Object.assign(new Error(message), { status });
  const cases: [string, unknown, string | undefined][] = [
    ["a 404 is a note that is gone", withStatus(404, "not found"), "missing"],
    ["a 404 that is no Error (duck typing)", { status: 404 }, "missing"],
    [
      "a 500",
      withStatus(500, "Could not access the vault. Try again."),
      "Could not access the vault. Try again.",
    ],
    ["no network", withStatus(0, "Could not reach the server"), "Could not reach the server"],
    ["a plain Error", new Error("boom"), "boom"],
    ["an Error that says nothing", new Error(""), undefined],
    ["something that is no Error", "boom", undefined],
  ];
  for (const [name, rejection, detail] of cases) {
    const h = setup();
    startSave(h);
    h.saves[0].reject(rejection);
    await settle();
    assert.deepEqual(snapshot(h), ["error", "b", "r1"], name);
    assert.deepEqual(
      h.states.slice(-1),
      [detail === undefined ? "error" : `error:${detail}`],
      name,
    );
    h.timers.advance(60_000);
    await settle();
    assert.equal(h.saves.length, 1, name);
  }
});

test("a 409 is a conflict for any error that says so, with or without the revision", async () => {
  for (const [rejection, state] of [
    [Object.assign(new Error("changed"), { status: 409 }), "conflict"],
    [{ status: 409, currentRevision: "r7" }, "conflict:r7"],
  ] as const) {
    const h = setup();
    startSave(h);
    h.saves[0].reject(rejection);
    await settle();
    assert.equal(h.controller.state, "conflict");
    assert.equal(h.states.at(-1), state);
  }
});

test("a save that throws instead of rejecting is an error too", async () => {
  const states: string[] = [];
  const timers = fakeTimers();
  const controller = new SaveController(
    {
      debounceMs: 1000,
      save: () => {
        throw new Error("boom");
      },
      onstate: (state) => states.push(state),
      setTimer: timers.setTimer,
      clearTimer: timers.clearTimer,
    },
    { content: "a", revision: "r1" },
  );
  controller.edit("b");
  await controller.flush();
  assert.equal(controller.state, "error");
  // The next edit is not stuck behind the save that threw.
  controller.edit("bc");
  assert.equal(timers.pending, 1);
  await settle();
  assert.deepEqual(states, ["dirty", "saving", "error", "dirty"]);
});

test("an error is not final: the next edit, or a flush, saves again over the same revision", async () => {
  const h = setup();
  startSave(h);
  h.saves[0].reject(new Error("boom"));
  await settle();

  h.controller.edit("bc");
  assert.equal(h.controller.state, "dirty");
  h.timers.advance(1000);
  assert.deepEqual(sent(h), [
    ["b", "r1"],
    ["bc", "r1"],
  ]);
  h.saves[1].reject(new Error("boom"));
  await settle();

  const flushing = h.controller.flush(); // Mod-s retries without an edit
  await settle();
  assert.deepEqual(sent(h), [
    ["b", "r1"],
    ["bc", "r1"],
    ["bc", "r1"],
  ]);
  h.saves[2].resolve("r2");
  await flushing;
  assert.deepEqual(snapshot(h), ["clean", "bc", "r2"]);
  assert.deepEqual(h.states, [
    "dirty",
    "saving",
    "error:boom",
    "dirty",
    "saving",
    "error:boom",
    "saving",
    "clean",
  ]);
});

test("typing back to the saved text after an error leaves nothing to save", async () => {
  const h = setup();
  startSave(h);
  h.saves[0].reject(new Error("boom"));
  await settle();
  h.controller.edit("a");
  assert.equal(h.controller.state, "clean");
  assert.equal(h.timers.pending, 0);
});

test("external adopts the disk version when clean, and says so quietly", async () => {
  const h = setup();
  assert.equal(h.controller.external("theirs", "r2"), "applied");
  assert.deepEqual(snapshot(h), ["clean", "theirs", "r2"]);
  await settle();
  assert.deepEqual(h.states, []);

  // The next save is over the revision that was adopted, of the text that was adopted.
  h.controller.edit("theirs+");
  h.timers.advance(1000);
  assert.deepEqual(sent(h), [["theirs+", "r2"]]);
  h.saves[0].resolve("r3");
  await settle();
  assert.equal(h.controller.external("theirs again", "r4"), "applied");
  assert.deepEqual(snapshot(h), ["clean", "theirs again", "r4"]);
});

test("external is a conflict when there is unsaved or unconfirmed text, and changes nothing", async () => {
  const arrange: [string, (h: Harness) => unknown][] = [
    ["dirty", (h) => h.controller.edit("b")],
    ["saving", (h) => startSave(h)],
    [
      "error",
      async (h) => {
        startSave(h);
        h.saves[0].reject(new Error("boom"));
        await settle();
      },
    ],
    ["conflict", (h) => loseSave(h)],
  ];
  for (const [name, prepare] of arrange) {
    const h = setup();
    await prepare(h);
    const before = snapshot(h);
    assert.equal(h.controller.external("theirs", "r2"), "conflict", name);
    assert.deepEqual(snapshot(h), before, name); // local text and revision stay as they were
    assert.deepEqual(before[1], "b", name);
  }
});

test("overwrite saves the current text over the revision it is given, and ends clean", async () => {
  const h = setup();
  await loseSave(h);
  h.controller.edit("b!"); // typed while the banner was up
  h.timers.advance(60_000);
  assert.equal(h.saves.length, 1);

  const overwriting = h.controller.overwrite("r9");
  await settle();
  assert.deepEqual(sent(h), [
    ["b", "r1"],
    ["b!", "r9"],
  ]);
  assert.equal(h.controller.state, "saving");
  h.saves[1].resolve("r10");
  await overwriting;
  assert.deepEqual(snapshot(h), ["clean", "b!", "r10"]);
  assert.deepEqual(h.states, ["dirty", "saving", "conflict:r9", "saving", "clean"]);
  h.timers.advance(60_000);
  assert.equal(h.saves.length, 2);
});

test("an overwrite that loses too is a conflict again, over the newer revision, with no retry", async () => {
  const h = setup();
  await loseSave(h);
  const overwriting = h.controller.overwrite("r9");
  await settle();
  h.saves[1].reject(conflicted("r11"));
  await overwriting;
  // The revision of the controller moves only when a save is made, or a version is adopted.
  assert.deepEqual(snapshot(h), ["conflict", "b", "r1"]);
  assert.equal(h.states.at(-1), "conflict:r11");
  h.timers.advance(60_000);
  await settle();
  assert.equal(h.saves.length, 2);
});

test("overwrite saves even text that is saved already: the disk may have been changed behind that save", async () => {
  const h = setup();
  startSave(h, "mine");
  // A poll noticed a change while the save was out, and the save went through all the same.
  assert.equal(h.controller.external("theirs", "r2"), "conflict");
  h.saves[0].resolve("r3");
  await settle();
  assert.deepEqual(snapshot(h), ["clean", "mine", "r3"]);

  const overwriting = h.controller.overwrite("r2");
  await settle();
  assert.deepEqual(sent(h), [
    ["mine", "r1"],
    ["mine", "r2"],
  ]);
  h.saves[1].resolve("r4");
  await overwriting;
  assert.deepEqual(snapshot(h), ["clean", "mine", "r4"]);
});

test("an overwrite asked while a save is out goes after it: never two saves at once", async () => {
  const h = setup();
  startSave(h);
  const overwriting = h.controller.overwrite("r9");
  await settle();
  assert.equal(h.saves.length, 1);
  h.saves[0].reject(conflicted("r9")); // the save that was out lost
  await settle();
  assert.deepEqual(sent(h), [
    ["b", "r1"],
    ["b", "r9"],
  ]);
  h.saves[1].resolve("r10");
  await overwriting;
  assert.deepEqual(snapshot(h), ["clean", "b", "r10"]);
});

test("reload adopts the disk version, drops local edits and the autosave that was waiting", async () => {
  const h = setup();
  h.controller.edit("mine");
  h.controller.reload("theirs", "r2");
  assert.deepEqual(snapshot(h), ["clean", "theirs", "r2"]);
  assert.equal(h.timers.pending, 0);
  h.timers.advance(60_000);
  await h.controller.flush();
  assert.equal(h.saves.length, 0);
  await settle();
  assert.deepEqual(h.states, ["dirty", "clean"]);

  // From a conflict too, and what is saved next is over what was loaded.
  const lost = setup();
  await loseSave(lost);
  lost.controller.reload("theirs", "r9");
  assert.deepEqual(snapshot(lost), ["clean", "theirs", "r9"]);
  lost.controller.edit("theirs+");
  lost.timers.advance(1000);
  assert.deepEqual(sent(lost), [
    ["b", "r1"],
    ["theirs+", "r9"],
  ]);
});

test("reload while a save is out: its late answer changes nothing, and the next edit saves", async () => {
  const answers: [string, (save: Save) => void][] = [
    ["is saved", (save) => save.resolve("r9")],
    ["loses", (save) => save.reject(conflicted("r8"))],
    ["fails", (save) => save.reject(new Error("boom"))],
  ];
  for (const [name, answer] of answers) {
    const h = setup();
    startSave(h, "mine");
    h.controller.reload("theirs", "r2");
    answer(h.saves[0]);
    await settle();
    assert.deepEqual(snapshot(h), ["clean", "theirs", "r2"], name);
    assert.deepEqual(h.states, ["dirty", "saving", "clean"], name);

    // Not stuck behind the save that was left out.
    h.controller.edit("theirs+");
    h.timers.advance(1000);
    assert.deepEqual(
      sent(h),
      [
        ["mine", "r1"],
        ["theirs+", "r2"],
      ],
      name,
    );
    h.saves[1].resolve("r3");
    await settle();
    assert.deepEqual(snapshot(h), ["clean", "theirs+", "r3"], name);
  }
});

test("onstate hears every change in order, and never from inside edit", async () => {
  const h = setup();
  h.states.push("edit(");
  h.controller.edit("b");
  h.states.push(")");
  // The state is there at once; the news comes after the call has returned.
  assert.equal(h.controller.state, "dirty");
  await settle();
  h.timers.advance(1000);
  h.saves[0].resolve("r2");
  await settle();
  h.states.push("edit(");
  h.controller.edit("a"); // "b" is what was saved, so this is unsaved again
  h.states.push(")");
  await settle();
  assert.deepEqual(h.states, ["edit(", ")", "dirty", "saving", "clean", "edit(", ")", "dirty"]);
});

test("without injected timers it uses setTimeout and clearTimeout", async () => {
  const saved: string[] = [];
  const controller = new SaveController(
    {
      debounceMs: 1,
      save: async (content) => {
        saved.push(content);
        return { revision: "r2" };
      },
      onstate: () => {},
    },
    { content: "a", revision: "r1" },
  );
  controller.edit("b");
  controller.edit("bc");
  await new Promise((resolve) => setTimeout(resolve, 50));
  assert.deepEqual(saved, ["bc"]);
  assert.equal(controller.state, "clean");
});
