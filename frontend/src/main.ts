import { mount } from "svelte";
import "./app.css";
import "./design-system/theme/tokens.css";
import "./design-system/theme/components.css";
import "./design-system/custom/theme-toggle/theme-toggle.css";
import "./editor.css";
import App from "./App.svelte";

mount(App, { target: document.getElementById("app")! });
