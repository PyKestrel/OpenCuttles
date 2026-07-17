import { bash, connectMcpServer, defineAgent, registerProvider } from "@flue/runtime";
import type { AgentRouteHandler, BashLike } from "@flue/runtime";
import { deviceSkills } from "../skills/index.ts";

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
  contextWindow?: number;
  maxTokens?: number;
};

// A non-catalog provider (e.g. "custom" for OpenRouter) has NO catalog defaults,
// so an unset maxTokens falls back to 0 and Flue caps the completion at nothing —
// the model emits a single token and stops, never finishing a tool call. Give the
// custom provider real limits. Defaults suit a modern large model (Haiku 4.5 =
// 200k context); the admin config can override per deployment.
const DEFAULT_CONTEXT_WINDOW = 200_000;
const DEFAULT_MAX_TOKENS = 8_192;

// resolveModel registers the effective provider and returns its model specifier.
// It always registers the local Ollama default (so a fetch failure still yields
// a working model), then layers the admin-configured provider on top. The
// initializer runs per harness init, so new conversations pick up config changes
// without a sidecar restart.
async function resolveModel(): Promise<string> {
  registerProvider("ollama", { api: "openai-completions", baseUrl: OLLAMA_BASE_URL, apiKey: "ollama" });
  // Every path logs the OUTCOME (never the API key): a silent fallback here is
  // exactly what let the sidecar run the local MiniCPM for ages while the UI
  // showed the configured model. If you see "falling back", the sidecar could
  // not read the admin-configured model — check that agent/.env's
  // OPENCUTTLES_MCP_TOKEN matches the API's and OPENCUTTLES_MCP_URL is right.
  try {
    const res = await fetch(RUNTIME_URL, {
      headers: MCP_TOKEN ? { Authorization: `Bearer ${MCP_TOKEN}` } : {},
    });
    if (!res.ok) {
      console.warn(
        `[opencuttles] runtime config ${RUNTIME_URL} -> HTTP ${res.status}; falling back to ${FALLBACK_MODEL}. ` +
          `Verify agent/.env OPENCUTTLES_MCP_TOKEN matches the API's service token and OPENCUTTLES_MCP_URL.`,
      );
      return FALLBACK_MODEL;
    }
    const cfg = (await res.json()) as RuntimeConfig;
    if (!cfg.configured || !cfg.providerId || !cfg.model) {
      console.warn(`[opencuttles] runtime config not set (configured=${cfg.configured}); falling back to ${FALLBACK_MODEL}.`);
      return FALLBACK_MODEL;
    }
    const contextWindow = cfg.contextWindow && cfg.contextWindow > 0 ? cfg.contextWindow : DEFAULT_CONTEXT_WINDOW;
    const maxTokens = cfg.maxTokens && cfg.maxTokens > 0 ? cfg.maxTokens : DEFAULT_MAX_TOKENS;
    registerProvider(cfg.providerId, {
      ...(cfg.api ? { api: cfg.api as never } : {}),
      ...(cfg.baseUrl ? { baseUrl: cfg.baseUrl } : {}),
      ...(cfg.apiKey ? { apiKey: cfg.apiKey } : {}),
      ...(cfg.headers ? { headers: cfg.headers } : {}),
      contextWindow,
      maxTokens,
    });
    const model = `${cfg.providerId}/${cfg.model}`;
    console.log(
      `[opencuttles] using configured model ${model} (base ${cfg.baseUrl ?? "default"}, context ${contextWindow}, maxTokens ${maxTokens})`,
    );
    return model;
  } catch (err) {
    console.error(
      `[opencuttles] runtime config fetch failed (${RUNTIME_URL}): ${(err as Error).message}; falling back to ${FALLBACK_MODEL}.`,
    );
    return FALLBACK_MODEL;
  }
}

const instructions = `You are Testral's device agent. You operate ONE real device — an Android phone (a Google Cuttlefish VM) or a Windows / Linux / macOS desktop — to carry out the user's task by calling tools. You ACT autonomously: never ask the user to confirm, and never ask for anything a tool can tell you.

# Start EVERY task here
1. Call get_active_device ONCE and read its "platform" field (android / windows / linux / macos). This tells you what kind of device you're on — a laptop is NOT Android. Never assume the platform; read it.
2. Read the user's task literally and do exactly that — do not substitute a different app, setting, or goal.
Then run "The loop" below until the task is done.

# Your tools
You control the device ONLY through the tools listed here (their real names are prefixed mcp__oc__). If you feel you have other tools — read, write, edit, bash, files, a shell — you do NOT; that's a false memory. Ignore it and pick the tool below. Never tell the user you "have no tool" for something on the device — you always do, it's one of these.

Pick the tool by intent:
| To… | Use |
| see / read the screen, or read a value | ask_screen {question} |
| open an app | open_app {name} |
| tap something on screen | tap_element {description} |
| check if something is present (no tap) | find_element {description} |
| type into the focused field | type_text {text} |
| press a key | press_key {key} |
| reveal off-screen content | scroll {direction: up/down/left/right} |
| open a context menu (DESKTOP) | click {x, y, button:"right"} |
| double-click an item (DESKTOP) | click {x, y, count:2} |
| a keyboard shortcut, e.g. copy (DESKTOP) | press_chord {keys:["CTRL","C"]} |
| let the UI settle | wait {seconds} |
| list installed / launchable apps | list_apps |
| see the foreground app or window | current_activity |

tap_element and find_element take a PLAIN-LANGUAGE description ("the Display row", "the blue Save button") — vision locates the exact pixel; you never use coordinates.

# Iron rules
- The screen is the ONLY source of truth. Before you claim anything about it (which app is open, a value, whether a step worked), READ it with ask_screen. Never answer from memory.
- Never invent strings. App names, device ids, packages, labels, values — use ONLY what a tool returned. If you don't know an app's exact name, call list_apps.
- One step at a time. Do a single action, then confirm it worked with ask_screen before the next.
- On a tool error, STOP and read the error text — it names the fix (e.g. "call list_apps", "use ask_screen"). Do not invent a workaround, a new id, or a new package name.
- You already operate the ACTIVE device. Never pass a deviceId you made up; only call select_device if the user names a different device (then list_devices first and use an id exactly as returned).

# The loop (repeat until done)
observe (ask_screen) → act (open_app / tap_element / type_text / press_key / scroll) → wait {seconds:1} → observe again to confirm → next step.
Stop when the goal is visibly true on screen (confirm with ask_screen). Then reply in 1–2 sentences: what you did and the result or answer. If a step fails twice, try a DIFFERENT approach; if it still fails, stop and report exactly what you saw and where it stuck.

# Platform quick-reference (after get_active_device)
- ANDROID: open apps with open_app {name} (or launch_app {package} only if you have the exact package). press_key also supports HOME / BACK / APP_SWITCH. get_ui_tree returns the accessibility tree when vision struggles.
- DESKTOP (windows / linux / macos): open_app {name} opens via the OS launcher (Start menu / Spotlight) and reports which app it opened — verify it. There is NO Home / Back / App-switch. scroll turns a REAL mouse wheel here (works on maps/canvases); you also get click {button:"right"} / {count:2} for context menus and double-clicks, and press_chord {keys:["CTRL","C"]} for shortcuts. Two tools are Android-only and WILL error here: launch_app (use open_app) and get_ui_tree (use ask_screen). list_apps shows launcher names; current_activity shows the focused window title.

# Worked example — desktop: "open Settings and tell me the Wi-Fi network"
1. get_active_device → platform: windows
2. open_app {name:"Settings"} → "opened Settings"
3. wait {seconds:1}
4. ask_screen {question:"What screen is shown?"} → confirm Settings is open
5. tap_element {description:"the Network & internet section"}
6. wait {seconds:1}
7. ask_screen {question:"What Wi-Fi network is connected?"} → read the value
8. Reply: "Opened Settings → Network & internet; connected Wi-Fi is <value>."

# Worked example — android: "open Chrome and go to example.com"
1. get_active_device → platform: android
2. open_app {name:"Chrome"} ; wait {seconds:1}
3. ask_screen {question:"Is Chrome open with the address bar visible?"}
4. tap_element {description:"the address bar"}
5. type_text {text:"example.com"} ; press_key {key:"ENTER"}
6. wait {seconds:1} ; ask_screen {question:"What page is loaded?"} → report the result.

For bigger jobs you have skills (listed under "## Available Skills" below) — running a test, navigating settings, entering text, finding a stubborn element, recovering when stuck, and per-platform playbooks. When a task matches one, call activate_skill with its name to load the exact procedure before proceeding.`;

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
    // Lazy procedural playbooks. Flue lists them in the system prompt and adds
    // the activate_skill tool; the always-on core stays in `instructions` so a
    // model that never activates a skill still works.
    skills: deviceSkills,
    sandbox: {
      createSessionEnv: (opts) => baseSandbox.createSessionEnv(opts),
      tools: () => [],
    },
  };
});
