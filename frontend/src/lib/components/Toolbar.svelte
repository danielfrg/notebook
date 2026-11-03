<script lang="ts">
  import { IconButton } from "@kenn-io/kit-ui";
  import BoldIcon from "@lucide/svelte/icons/bold";
  import ItalicIcon from "@lucide/svelte/icons/italic";
  import HeadingIcon from "@lucide/svelte/icons/heading-2";
  import ListIcon from "@lucide/svelte/icons/list";
  import ListChecksIcon from "@lucide/svelte/icons/list-checks";
  import LinkIcon from "@lucide/svelte/icons/link";
  import StatusPill from "./StatusPill.svelte";
  import CopyIcon from "@lucide/svelte/icons/copy";
  import UndoIcon from "@lucide/svelte/icons/undo-2";
  import RedoIcon from "@lucide/svelte/icons/redo-2";
  import type { EditorCommand } from "../editor/commands.ts";

  let { run, oncopy, disabled = false }: { run: (command: EditorCommand) => void; oncopy: () => void; disabled?: boolean } = $props();
</script>

<div class="note-toolbar" role="toolbar" aria-label="Formatting">
  <div class="toolbar-group">
  <IconButton ariaLabel="Bold" {disabled} onclick={() => run("bold")}><BoldIcon size={15} /></IconButton>
  <IconButton ariaLabel="Italic" {disabled} onclick={() => run("italic")}><ItalicIcon size={15} /></IconButton>
  <IconButton ariaLabel="Heading" {disabled} onclick={() => run("heading")}><HeadingIcon size={15} /></IconButton>
  </div>
  <span class="toolbar-divider" aria-hidden="true"></span>
  <div class="toolbar-group">
  <IconButton ariaLabel="Bulleted list" {disabled} onclick={() => run("bullet")}><ListIcon size={15} /></IconButton>
  <IconButton ariaLabel="Task list" {disabled} onclick={() => run("task")}><ListChecksIcon size={15} /></IconButton>
  <IconButton ariaLabel="Link" {disabled} onclick={() => run("link")}><LinkIcon size={15} /></IconButton>
  </div>
  <span class="toolbar-spacer"></span>
  <StatusPill />
  <IconButton ariaLabel="Copy note as Markdown" title="Copy as Markdown" onclick={oncopy}><CopyIcon size={15} /></IconButton>
  <IconButton ariaLabel="Undo" {disabled} onclick={() => run("undo")}><UndoIcon size={15} /></IconButton>
  <IconButton ariaLabel="Redo" {disabled} onclick={() => run("redo")}><RedoIcon size={15} /></IconButton>
</div>
