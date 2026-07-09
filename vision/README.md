# OpenCuttles vision sidecar

Serves a local **Moondream** VLM so OpenCuttles can *ground* and *interpret* device
screenshots. The Go backend uses it two ways: Moondream-backed MCP tools that give
the natural-language agent "eyes" (`tap_element`, `find_element`, `ask_screen`), and
the deterministic test runner's per-step grounding + assertions.

CPU-only. Default model is **Moondream 2 0.5B int4** (~375 MB, ~816 MB RAM).

## Setup

```bash
cd vision
python3 -m venv .venv && . .venv/bin/activate
pip install -r requirements.txt

# Download the model weight (0.5B int4). Swap for the 2B for better accuracy.
wget https://huggingface.co/vikhyatk/moondream2/resolve/main/moondream-0_5b-int4.mf.gz
```

## Run

```bash
OPENCUTTLES_VISION_MODEL=./moondream-0_5b-int4.mf.gz \
  uvicorn server:app --host 127.0.0.1 --port 8791
```

Install as a service with `deploy/systemd/opencuttles-vision.service`, then
`sudo systemctl enable --now opencuttles-vision`.

## API

- `POST /point {image, target}` → `{"points": [{"x","y"}]}` — coordinates normalized 0–1.
- `POST /query {image, question}` → `{"answer": "..."}` — visual Q&A.
- `GET /healthz` → `{"status","model"}`.

`image` is a base64-encoded PNG.

## Swapping accuracy for size

Moondream's own docs note the 0.5B has limited out-of-box accuracy (it's a
distillation target). If Android-UI grounding is unreliable, download the 2B weight
and point `OPENCUTTLES_VISION_MODEL` at it — no code change:

```bash
wget https://huggingface.co/vikhyatk/moondream2/resolve/main/moondream-2b-int8.mf.gz
```
