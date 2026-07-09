# OpenCuttles vision sidecar

Serves a local **Florence-2** VLM so OpenCuttles can *ground* and *interpret* device
screenshots. The Go backend uses it two ways: Moondream-style MCP tools that give
the natural-language agent "eyes" (`tap_element`, `find_element`, `ask_screen`), and
the deterministic test runner's per-step grounding + assertions.

CPU-only. Default model is **microsoft/Florence-2-base** (~0.23B, ~0.5 GB weights).

> Why Florence-2 and not Moondream? Moondream's tiny 0.5B model no longer runs on
> CPU — its current packages require a GPU ("Photon") and the old CPU-onnx weights
> are incompatible with the maintained client. Florence-2-base is a maintained,
> genuinely tiny VLM with strong open-vocabulary detection (grounding) on CPU.

## Setup

```bash
cd vision
./install.sh          # venv + CPU torch/torchvision + transformers, etc.
```

## Run

```bash
.venv/bin/uvicorn server:app --host 127.0.0.1 --port 8791
```

Install as a service with `deploy/systemd/opencuttles-vision.service`, then
`sudo systemctl enable --now opencuttles-vision`. The model downloads to the
HuggingFace cache on first run.

## API (model-agnostic contract)

- `POST /point {image, target}` → `{"points": [{"x","y"}]}` — box centers, normalized
  0–1. Backed by Florence-2 `<OPEN_VOCABULARY_DETECTION>`.
- `POST /query {image, question}` → `{"answer": "..."}` — Florence-2 has no free-form
  VQA, so this returns a detailed caption + on-screen OCR text for the caller to
  reason over (good for presence/text/state questions).
- `GET /healthz` → `{"status","model"}`.

`image` is a base64-encoded PNG.

## Accuracy vs size

Point `OPENCUTTLES_VISION_MODEL` at `microsoft/Florence-2-large` (~0.77B) for better
grounding at more cost — no code change.
