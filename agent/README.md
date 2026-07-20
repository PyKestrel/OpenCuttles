# OpenCuttles agent sidecar

A [Flue](https://flueframework.com)-harnessed agent that drives devices in natural
language — Android over ADB, and Windows/Linux/macOS desktops through the
dial-home runner. It acts through the Testral **MCP** device tools (screenshot,
UI tree, tap, click, type, press_chord, launch_app, …), perceiving the screen as
the accessibility tree plus vision grounding. The API reverse-proxies this
sidecar's HTTP endpoints under `/agents/*` for the dashboard chat panel.

**The model is configured by an admin in the dashboard**, not here: the sidecar
reads the provider, model, and decrypted API key from
`GET /api/v1/agent/runtime` at startup. A local **MiniCPM5-1B** on Ollama is the
zero-config fallback when nothing is configured — convenient for local
development, but any OpenAI-, Anthropic-, Google-, or Azure-compatible provider
works.

Requires **Node ≥ 22.18** (the Flue CLI needs native TypeScript config support).

## Setup

```bash
cd agent
npm install
cp .env.example .env         # then set OPENCUTTLES_MCP_TOKEN to match the backend
```

Pull the model and make sure Ollama is running:

```bash
ollama pull openbmb/minicpm5
```

The Ollama provider is registered in code (`agents/opencuttles.ts` via
`registerProvider`), so no `~/.pi` config is required. `models.json` is kept only
for use with the standalone `pi` CLI.

## Run

Development (watch mode):

```bash
npx flue dev --port 8790
```

Production (build once, then run the server — this is what the systemd unit does):

```bash
npx flue build --target node
PORT=8790 node dist/server.mjs
```

Install as a service with `deploy/systemd/opencuttles-agent.service` (adjust the
`User`/`WorkingDirectory`), then `sudo systemctl enable --now opencuttles-agent`.

## Swapping the model

The 1B core is fine for simple, single-step requests but is limited on long
multi-step GUI tasks. Point `OPENCUTTLES_AGENT_MODEL` at any Ollama model
(`ollama/<tag>`) or another Pi-supported provider to trade capability for size —
no code change needed.

## Test one task without the UI

```bash
npx flue run opencuttles --input '{"message":"list the devices and their states"}'
```
