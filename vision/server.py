"""OpenCuttles vision sidecar (Florence-2).

Serves a local, CPU-friendly Florence-2 VLM over HTTP so the OpenCuttles backend
can ground and interpret device screenshots. Two skills, keeping a model-agnostic
contract shared by the agent's vision MCP tools and the test runner:

  POST /point  {image, target}   -> {"points": [{"x","y"}]}   (normalized 0-1)
  POST /query  {image, question} -> {"answer": "..."}

`image` is a base64-encoded PNG. Point grounds text UI labels with OCR-with-region
(match the target against recognized on-screen text, return that box's center),
which is far more reliable than open-vocabulary detection on terse labels; it falls
back to open-vocab detection (whole-image boxes filtered) for non-text targets like
icons. Query has no free-form VQA in Florence-2, so it returns a detailed caption
plus on-screen OCR text — good for presence/text/state questions the caller reasons
over.

Default model is microsoft/Florence-2-base (~0.23B), CPU. Override with
OPENCUTTLES_VISION_MODEL (e.g. microsoft/Florence-2-large for more accuracy).
"""

import base64
import binascii
import io
import os
import re
import threading
from contextlib import asynccontextmanager

import torch
from fastapi import FastAPI, HTTPException
from PIL import Image
from pydantic import BaseModel
from transformers import AutoModelForCausalLM, AutoProcessor

MODEL_ID = os.environ.get("OPENCUTTLES_VISION_MODEL", "microsoft/Florence-2-base")

_model = None
_processor = None
# Generation is CPU-bound; serialize (low concurrency: one agent + one runner).
_lock = threading.Lock()


@asynccontextmanager
async def lifespan(_app: FastAPI):
    global _model, _processor
    _model = AutoModelForCausalLM.from_pretrained(
        MODEL_ID, trust_remote_code=True, torch_dtype=torch.float32
    ).eval()
    _processor = AutoProcessor.from_pretrained(MODEL_ID, trust_remote_code=True)
    yield


app = FastAPI(title="opencuttles-vision", lifespan=lifespan)


class PointRequest(BaseModel):
    image: str
    target: str


class QueryRequest(BaseModel):
    image: str
    question: str


def _decode_image(image_b64: str) -> Image.Image:
    if image_b64.startswith("data:"):
        image_b64 = image_b64.split(",", 1)[-1]
    try:
        raw = base64.b64decode(image_b64)
        return Image.open(io.BytesIO(raw)).convert("RGB")
    except (binascii.Error, ValueError, OSError) as exc:
        raise HTTPException(status_code=400, detail=f"invalid image: {exc}") from exc


def _run(image: Image.Image, task: str, text: str = "") -> dict:
    prompt = task + text
    inputs = _processor(text=prompt, images=image, return_tensors="pt")
    with torch.no_grad():
        generated = _model.generate(
            input_ids=inputs["input_ids"],
            pixel_values=inputs["pixel_values"],
            max_new_tokens=512,
            # Greedy decode: Florence-2's structured skills don't need beam
            # search, and on a CPU host shared with the emulator, halving the
            # compute both speeds grounding and eases contention that would
            # otherwise leave the device unresponsive to injected taps.
            num_beams=1,
            do_sample=False,
        )
    decoded = _processor.batch_decode(generated, skip_special_tokens=False)[0]
    return _processor.post_process_generation(
        decoded, task=task, image_size=(image.width, image.height)
    )


def _tokens(text: str) -> list:
    # Lowercase, treat "&" as "and", keep only alphanumerics as tokens.
    return re.sub(r"[^a-z0-9 ]+", " ", text.lower().replace("&", "and")).split()


def _ocr_locate(image: Image.Image, target: str):
    """Return the normalized center of the on-screen text best matching target,
    or None. A candidate matches when it contains every target token; the most
    specific (fewest extra tokens) candidate wins."""
    want = _tokens(target)
    if not want:
        return None
    result = _run(image, "<OCR_WITH_REGION>").get("<OCR_WITH_REGION>", {})
    quads = result.get("quad_boxes", [])
    labels = result.get("labels", [])
    best, best_extra = None, 1 << 30
    for quad, label in zip(quads, labels):
        have = _tokens(str(label))
        if have and set(want).issubset(have):
            extra = len(have) - len(want)
            if extra < best_extra:
                best, best_extra = quad, extra
    if best is None:
        return None
    xs, ys = best[0::2], best[1::2]
    return {"x": (sum(xs) / len(xs)) / image.width, "y": (sum(ys) / len(ys)) / image.height}


@app.get("/healthz")
def healthz():
    return {"status": "ok" if _model is not None else "loading", "model": MODEL_ID}


@app.post("/point")
def point(req: PointRequest):
    image = _decode_image(req.image)
    with _lock:
        # Text labels dominate Android UI: match them via OCR regions first.
        hit = _ocr_locate(image, req.target)
        if hit is not None:
            return {"points": [hit]}
        # Fall back to open-vocabulary detection for icons / non-text targets.
        result = _run(image, "<OPEN_VOCABULARY_DETECTION>", req.target)
    boxes = result.get("<OPEN_VOCABULARY_DETECTION>", {}).get("bboxes", [])
    area = image.width * image.height
    points = []
    for box in boxes:
        x1, y1, x2, y2 = box
        # Drop degenerate whole-image boxes (Florence-2's "I can't localize").
        if (x2 - x1) * (y2 - y1) > 0.8 * area:
            continue
        points.append(
            {"x": ((x1 + x2) / 2) / image.width, "y": ((y1 + y2) / 2) / image.height}
        )
    return {"points": points}


@app.post("/query")
def query(req: QueryRequest):
    image = _decode_image(req.image)
    with _lock:
        caption = _run(image, "<MORE_DETAILED_CAPTION>").get("<MORE_DETAILED_CAPTION>", "")
        ocr = _run(image, "<OCR>").get("<OCR>", "")
    answer = (caption or "").strip()
    if ocr.strip():
        answer = f"{answer}\nOn-screen text: {ocr.strip()}"
    return {"answer": answer.strip()}
