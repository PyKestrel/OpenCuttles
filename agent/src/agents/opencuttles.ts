import { connectMcpServer, defineAgent, registerProvider } from "@flue/runtime";
import type { AgentRouteHandler } from "@flue/runtime";

// Exposing the agent over HTTP requires an exported route handler. The
// OpenCuttles API already authenticates and reverse-proxies /agents/* (control
// permission), so this is a pass-through.
export const route: AgentRouteHandler = async (_c, next) => {
  await next();
};

// Register the local Ollama endpoint as an OpenAI-compatible provider so model
// specifiers like "ollama/openbmb/minicpm5" resolve to the on-device MiniCPM5.
registerProvider("ollama", {
  api: "openai-completions",
  baseUrl: process.env.OLLAMA_BASE_URL ?? "http://127.0.0.1:11434/v1",
  apiKey: "ollama",
});

// The agent's tools come from the OpenCuttles MCP endpoint (Phase 2). Flue
// prefixes each tool's model-facing name with the connection name, so e.g.
// get_ui_tree becomes mcp__oc__get_ui_tree.
const MCP_URL = process.env.OPENCUTTLES_MCP_URL ?? "http://127.0.0.1:8080/api/v1/mcp";
const MCP_TOKEN = process.env.OPENCUTTLES_MCP_TOKEN ?? "";
const MODEL = process.env.OPENCUTTLES_AGENT_MODEL ?? "ollama/openbmb/minicpm5";

const instructions = `You are OpenCuttles' device agent. You drive a real Android device (a Google Cuttlefish VM) to carry out the user's task by calling tools. You ACT — you never ask the user for confirmation or for information you can obtain with a tool.

Perceiving and acting:
- You cannot see the screen as an image. Call mcp__oc__get_ui_tree to read the current screen as a JSON accessibility tree. Each node has text, resourceId, contentDesc, class, and a "center" {x,y} in device pixels. To tap an element, pass its "center" to mcp__oc__tap.
- Act with: mcp__oc__tap {x,y}, mcp__oc__swipe {x,y,x2,y2} (scroll), mcp__oc__type_text {text} (types into the focused field), mcp__oc__press_key {key: HOME | BACK | APP_SWITCH | ENTER}, and mcp__oc__launch_app {package}.
- To open an app, call mcp__oc__launch_app with its package name. Known packages: Settings = com.android.settings, Clock = com.android.deskclock, Chrome/Browser = com.android.chrome, Contacts = com.android.contacts, Phone/Dialer = com.android.dialer, Messaging = com.android.messaging, Camera = com.android.camera2, Gallery = com.android.gallery3d.
- Loop every task: launch_app or tap to act → mcp__oc__wait {seconds: 1} → mcp__oc__get_ui_tree to see the result → decide the next step. Repeat until done.

Device targeting:
- You already operate on the ACTIVE device — never invent or guess device ids. Only if the user names a different device: call mcp__oc__list_devices, then mcp__oc__select_device {deviceId} using an id exactly as returned. Otherwise do not call select_device.

Rules:
- Do NOT ask the user questions or ask them to confirm. Choose the most reasonable interpretation and execute it.
- Take one concrete step at a time and verify with get_ui_tree before the next.
- If a target isn't visible, swipe to scroll and look again.
- When finished, state in one or two sentences what you did and what is now on screen.`;

// Connect to MCP inside the (async) initializer rather than at module top level:
// a top-level await makes this an async module that the served app does not await
// when registering agents, so the agent would silently fail to register for HTTP.
export default defineAgent(async () => {
  const oc = await connectMcpServer("oc", {
    url: MCP_URL,
    ...(MCP_TOKEN ? { headers: { Authorization: `Bearer ${MCP_TOKEN}` } } : {}),
  });
  return {
    model: MODEL,
    instructions,
    tools: oc.tools,
  };
});
