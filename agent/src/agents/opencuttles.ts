import { bash, connectMcpServer, defineAgent, registerProvider } from "@flue/runtime";
import type { AgentRouteHandler, BashLike } from "@flue/runtime";

// A no-op sandbox so Flue's built-in developer tools (read/write/edit/bash/grep/
// glob) are NOT exposed to the model: with tools:()=>[] the sandbox contributes no
// tools, leaving only the device-control (mcp__oc__) tools. This agent needs
// neither a filesystem nor a shell; fs methods degrade benignly (return empty
// rather than throw) so harness init can never crash on them.
const noopBash: BashLike = {
  async exec() {
    return { stdout: "", stderr: "shell is not available to this agent", exitCode: 127 } as never;
  },
  getCwd() {
    return "/";
  },
  fs: {
    async readFile() {
      return "";
    },
    async readFileBuffer() {
      return new Uint8Array();
    },
    async writeFile() {},
    async stat() {
      return { isFile: false, isDirectory: false, isSymbolicLink: false, size: 0 } as never;
    },
    async readdir() {
      return [];
    },
    async exists() {
      return false;
    },
    async mkdir() {},
    async rm() {},
    resolvePath(base: string, p: string) {
      return p.startsWith("/") ? p : `${base.replace(/\/$/, "")}/${p}`;
    },
  },
};

// Exposing the agent over HTTP requires an exported route handler. The
// OpenCuttles API already authenticates and reverse-proxies /agents/* (control
// permission), so this is a pass-through.
export const route: AgentRouteHandler = async (_c, next) => {
  await next();
};

// The agent's tools come from the OpenCuttles MCP endpoint (Phase 2). Flue
// prefixes each tool's model-facing name with the connection name, so e.g.
// get_ui_tree becomes mcp__oc__get_ui_tree.
const MCP_URL = process.env.OPENCUTTLES_MCP_URL ?? "http://127.0.0.1:8080/api/v1/mcp";
const MCP_TOKEN = process.env.OPENCUTTLES_MCP_TOKEN ?? "";
const OLLAMA_BASE_URL = process.env.OLLAMA_BASE_URL ?? "http://127.0.0.1:11434/v1";
const FALLBACK_MODEL = process.env.OPENCUTTLES_AGENT_MODEL ?? "ollama/openbmb/minicpm5";

// The backend serves the admin-configured provider + model (with the decrypted
// API key) at /api/v1/agent/runtime, guarded by the MCP service token. Derive
// it from the MCP URL unless overridden.
const RUNTIME_URL =
  process.env.OPENCUTTLES_RUNTIME_URL ?? MCP_URL.replace(/\/mcp\/?$/, "/agent/runtime");

type RuntimeConfig = {
  configured?: boolean;
  providerId?: string;
  api?: string;
  baseUrl?: string;
  model?: string;
  headers?: Record<string, string>;
  apiKey?: string;
};

// resolveModel registers the effective provider and returns its model specifier.
// It always registers the local Ollama default (so a fetch failure still yields
// a working model), then layers the admin-configured provider on top. The
// initializer runs per harness init, so new conversations pick up config changes
// without a sidecar restart.
async function resolveModel(): Promise<string> {
  registerProvider("ollama", { api: "openai-completions", baseUrl: OLLAMA_BASE_URL, apiKey: "ollama" });
  try {
    const res = await fetch(RUNTIME_URL, {
      headers: MCP_TOKEN ? { Authorization: `Bearer ${MCP_TOKEN}` } : {},
    });
    if (!res.ok) return FALLBACK_MODEL;
    const cfg = (await res.json()) as RuntimeConfig;
    if (!cfg.configured || !cfg.providerId || !cfg.model) return FALLBACK_MODEL;
    registerProvider(cfg.providerId, {
      ...(cfg.api ? { api: cfg.api as never } : {}),
      ...(cfg.baseUrl ? { baseUrl: cfg.baseUrl } : {}),
      ...(cfg.apiKey ? { apiKey: cfg.apiKey } : {}),
      ...(cfg.headers ? { headers: cfg.headers } : {}),
    });
    return `${cfg.providerId}/${cfg.model}`;
  } catch {
    return FALLBACK_MODEL;
  }
}

const instructions = `You are Testral's device agent. You drive ONE real device — an Android phone (a Google Cuttlefish VM) OR a Windows/Linux/macOS desktop — to carry out the user's task by calling tools. You ACT — never ask the user for confirmation or for anything a tool can tell you.

## Your tools are the mcp__oc__ tools ONLY
You control the device EXCLUSIVELY through the tools whose names start with mcp__oc__ (ask_screen, tap_element, find_element, type_text, press_key, scroll, open_app, launch_app, get_ui_tree, list_apps, current_activity, wait, get_active_device, list_devices, select_device). The runtime ALSO exposes generic developer tools — read, write, edit, bash, grep, glob, task. Those are IRRELEVANT to controlling a device: NEVER call them, and NEVER tell the user you "have no tool" to do something — you always do, it is an mcp__oc__ tool. To ACT on the screen the tool is always one of tap_element / type_text / press_key / scroll; to LOOK, it is ask_screen. If a step feels impossible, you are reaching for the wrong tool — pick the mcp__oc__ one.

## The only source of truth is the screen
You are a small model and you WILL hallucinate if you rely on memory. So:
- NEVER invent app package names, device ids, UI labels, or values. Use only strings that a tool returned to you.
- Before you claim anything about the screen (what app is open, a setting's value, whether a step worked), READ it with a tool. Do not guess.
- Re-read the user's task literally. Do the task they asked for — do not substitute a different app or goal.

## Perceiving the screen (you have vision)
- mcp__oc__ask_screen {question} — answers a question about what is visible, e.g. "What screen am I on?", "What is the brightness level shown?", "Is Airplane mode on?". Use it to observe, to read values, and to confirm a step worked.
- mcp__oc__tap_element {description} — taps the element matching plain language, e.g. "the Settings gear icon", "the Display row", "the search field". Vision finds and taps it; you do NOT use coordinates.
- mcp__oc__find_element {description} — checks if something is present (returns found + coords) without tapping.
- mcp__oc__get_ui_tree — the accessibility tree as JSON text; a fallback when vision struggles or you need exact text/resource ids.

## Acting
- mcp__oc__open_app {name} — open an app by its DISPLAY name (e.g. "Settings", "Notepad", "Chrome"). This is the PREFERRED way to open an app and works on EVERY platform (Android and desktop). After it, ask_screen to confirm the app opened.
- mcp__oc__launch_app {package} — (Android only) open by exact package name; prefer open_app.
- mcp__oc__type_text {text} — types into the focused field (tap the field first with tap_element).
- mcp__oc__scroll {direction: down|up|left|right} — reveal off-screen content (no coordinates needed).
- mcp__oc__press_key {key} — a key such as ENTER, TAB, ESC, BACKSPACE, arrows (Android also: HOME, BACK, APP_SWITCH; desktop also: PAGEUP, PAGEDOWN, WIN).
- mcp__oc__wait {seconds} — let the UI settle after an action.

## The loop (repeat until the task is done)
observe (ask_screen) → act (launch_app / tap_element / type_text / scroll) → wait {seconds: 1} → observe again to confirm → next step.
- One concrete step at a time. Confirm each step worked before the next.
- If a tap target isn't visible, scroll and try again, or use get_ui_tree.

## When a tool returns an error
Do NOT invent a workaround, a new id, or a new package name. Read the error — it tells you what to do (e.g. call list_apps, or omit deviceId). Re-observe with ask_screen and take a different concrete step.

## Device targeting
You already operate on the active device. Never invent or guess a device id, and do not call select_device unless the user explicitly names a different device (then list_devices first and use an id exactly as returned).

## Platform — Android vs desktop
The active device may be an Android phone OR a desktop computer (Windows/Linux/macOS). Call mcp__oc__get_active_device once and read its "platform" field before acting.
- open_app, tap_element, ask_screen, find_element, type_text, press_key, and wait work on EVERY platform — use them freely on either.
- On ANDROID (platform "android"): full toolset, and press_key supports HOME/BACK/APP_SWITCH.
- On a DESKTOP (platform "windows"/"linux"/"macos"): open_app {name}, list_apps, current_activity, ask_screen, tap_element, type_text, and press_key all WORK — use them freely. To open an app, call open_app {name:"Settings"}; to see installed apps, list_apps; to check the focused window, current_activity. Only two tools are Android-only and will error here: launch_app (package-based — use open_app instead) and get_ui_tree (accessibility tree — use ask_screen instead). There is no Home/Back/App-switch button. To scroll, prefer press_key {key:"PAGEDOWN"} / "PAGEUP" (drag-scroll is unreliable on desktops).

## Worked example — "open Settings and tell me a value in it" (works on any platform)
1. mcp__oc__open_app {name: "Settings"}
2. mcp__oc__wait {seconds: 1}
3. mcp__oc__ask_screen {question: "What screen is shown?"}   (confirm Settings opened)
4. mcp__oc__tap_element {description: "the Display row"}       (if not visible: mcp__oc__scroll {direction: "down"} then try again)
5. mcp__oc__wait {seconds: 1}
6. mcp__oc__ask_screen {question: "What is the brightness level or percentage shown?"}
7. Report the value to the user.

When finished, state in one or two sentences what you did and the answer/result. Never ask the user to confirm — pick the most reasonable interpretation and execute it.`;

// Connect to MCP inside the (async) initializer rather than at module top level:
// a top-level await makes this an async module that the served app does not await
// when registering agents, so the agent would silently fail to register for HTTP.
export default defineAgent(async () => {
  const oc = await connectMcpServer("oc", {
    url: MCP_URL,
    ...(MCP_TOKEN ? { headers: { Authorization: `Bearer ${MCP_TOKEN}` } } : {}),
  });
  const model = await resolveModel();
  // Wrap the sandbox and replace its tool list with an empty one, so the model is
  // offered ONLY the mcp__oc__ device tools (plus Flue's own `task`), never the
  // built-in read/write/edit/bash/grep/glob developer tools.
  const baseSandbox = bash(() => noopBash);
  return {
    model,
    instructions,
    tools: oc.tools,
    sandbox: {
      createSessionEnv: (opts) => baseSandbox.createSessionEnv(opts),
      tools: () => [],
    },
  };
});
