"""OpenCuttles vision sidecar (Florence-2).

Serves a local, CPU-friendly Florence-2 VLM over HTTP so the OpenCuttles backend
can ground and interpret device screenshots. Two skills, keeping a model-agnostic
contract shared by the agent's vision MCP tools and the test runner:

  POST /point  {image, target}   -> {"points": [{"x","y"}]}   (normalized 0-1)
  POST /query  {image, question} -> {"answer": "..."}

`image` is a base64-encoded PNG. Point uses Florence-2 open-vocabulary detection
and returns the box centers normalized to the image size. Query has no free-form
VQA in Florence-2, so it returns a detailed caption plus on-screen OCR text — good
for presence/text/state questions the caller reasons over.

Default model is microsoft/Florence-2-base (~0.23B), CPU. Override with
OPENCUTTLES_VISION_MODEL (e.g. microsoft/Florence-2-large for more accuracy).
"""

import base64
import binascii
import io
import os
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
            num_beams=3,
            do_sample=False,
        )
    decoded = _processor.batch_decode(generated, skip_special_tokens=False)[0]
    return _processor.post_process_generation(
        decoded, task=task, image_size=(image.width, image.height)
    )


@app.get("/healthz")
def healthz():
    return {"status": "ok" if _model is not None else "loading", "model": MODEL_ID}


@app.post("/point")
def point(req: PointRequest):
    image = _decode_image(req.image)
    with _lock:
        result = _run(image, "<OPEN_VOCABULARY_DETECTION>", req.target)
    parsed = result.get("<OPEN_VOCABULARY_DETECTION>", {})
    boxes = parsed.get("bboxes", [])
    points = []
    for box in boxes:
        x1, y1, x2, y2 = box
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
