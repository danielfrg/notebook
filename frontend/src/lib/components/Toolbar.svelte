<script lang="ts">
  import { Toolbar, ToolbarButton, ToolbarGroup, ToolbarSeparator } from "@ui-sdk/svelte/toolbar";
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

  let {
    run,
    oncopy,
    disabled = false,
  }: { run: (command: EditorCommand) => void; oncopy: () => void; disabled?: boolean } = $props();
</script>

<Toolbar class="note-toolbar" aria-label="Formatting">
  <ToolbarGroup aria-label="Text">
    <ToolbarButton aria-label="Bold" {disabled} onclick={() => run("bold")}
      ><BoldIcon /></ToolbarButton
    >
    <ToolbarButton aria-label="Italic" {disabled} onclick={() => run("italic")}
      ><ItalicIcon /></ToolbarButton
    >
    <ToolbarButton aria-label="Heading" {disabled} onclick={() => run("heading")}
      ><HeadingIcon /></ToolbarButton
    >
  </ToolbarGroup>
  <ToolbarSeparator />
  <ToolbarGroup aria-label="Blocks">
    <ToolbarButton aria-label="Bulleted list" {disabled} onclick={() => run("bullet")}
      ><ListIcon /></ToolbarButton
    >
    <ToolbarButton aria-label="Task list" {disabled} onclick={() => run("task")}
      ><ListChecksIcon /></ToolbarButton
    >
    <ToolbarButton aria-label="Link" {disabled} onclick={() => run("link")}
      ><LinkIcon /></ToolbarButton
    >
  </ToolbarGroup>
  <span class="grow"></span>
  <StatusPill />
  <ToolbarButton aria-label="Copy note as Markdown" title="Copy as Markdown" onclick={oncopy}
    ><CopyIcon /></ToolbarButton
  >
  <ToolbarButton aria-label="Undo" {disabled} onclick={() => run("undo")}
    ><UndoIcon /></ToolbarButton
  >
  <ToolbarButton aria-label="Redo" {disabled} onclick={() => run("redo")}
    ><RedoIcon /></ToolbarButton
  >
</Toolbar>
