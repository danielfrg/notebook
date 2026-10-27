import { mount } from "svelte";
import { initTheme, setThemeMode } from "@kenn-io/kit-ui";
import "@kenn-io/kit-ui/theme.css";
import "./theme.css";
import App from "./App.svelte";
import "./app.css";

const themeKey = "notebook-theme";

initTheme({ storageKey: themeKey });
// Start in dark mode while respecting an explicit light or system choice.
try {
  if (!localStorage.getItem(themeKey)) setThemeMode("dark");
} catch {
  setThemeMode("dark");
}

mount(App, { target: document.getElementById("app")! });
