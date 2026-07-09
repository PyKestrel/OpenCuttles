"""OpenCuttles vision sidecar.

Serves a local Moondream VLM over HTTP so the OpenCuttles backend can ground and
interpret device screenshots. Two skills are exposed:

  POST /point  {image, target}   -> {"points": [{"x","y"}]}   (normalized 0-1)
  POST /query  {image, question} -> {"answer": "..."}          (visual Q&A)

`image` is a base64-encoded PNG (data: URLs are tolerated). Coordinates come back
normalized to the image size; the caller scales them to device pixels.

The model is Moondream 2 0.5B int4 by default (~375 MB, CPU-only). Point
OPENCUTTLES_VISION_MODEL at a different weight file (e.g. the 2B int8) to trade
size for accuracy without code changes.
"""

import base64
import binascii
import io
import os
import threading
from contextlib import asynccontextmanager

import moondream as md
from fastapi import FastAPI, HTTPException
from PIL import Image
from pydantic import BaseModel

MODEL_PATH = os.environ.get("OPENCUTTLES_VISION_MODEL", "moondream-0_5b-int4.mf.gz")

# Moondream local inference is CPU-bound and not guaranteed thread-safe, so
# serialize model calls. Concurrency here is low (one agent + one test runner).
_model = None
_lock = threading.Lock()


@asynccontextmanager
async def lifespan(_app: FastAPI):
    global _model
    _model = md.vl(model=MODEL_PATH)
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


@app.get("/healthz")
def healthz():
    return {"status": "ok" if _model is not None else "loading", "model": MODEL_PATH}


@app.post("/point")
def point(req: PointRequest):
    image = _decode_image(req.image)
    with _lock:
        result = _model.point(image, req.target)
    return {"points": result.get("points", [])}


@app.post("/query")
def query(req: QueryRequest):
    image = _decode_image(req.image)
    with _lock:
        result = _model.query(image, req.question)
    return {"answer": result.get("answer", "")}
