# OpenCuttles agent sidecar

A [Flue](https://flueframework.com)-harnessed agent that drives Android devices in
natural language. It uses a local **MiniCPM5-1B** cognitive core (served by
Ollama) and the OpenCuttles **MCP** device tools (screenshot, UI tree, tap, type,
launch_app, …). The agent perceives the screen as the uiautomator accessibility
tree and acts through the tools; the OpenCuttles API reverse-proxies this
sidecar's HTTP endpoints under `/agents/*` for the WebUI chat panel.

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
