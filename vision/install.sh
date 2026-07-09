#!/usr/bin/env bash
# Set up the OpenCuttles vision sidecar (Florence-2, CPU-only).
#
# Uses a --without-pip venv bootstrapped via get-pip.py (Ubuntu ships python3
# without ensurepip/pip by default), and pins CPU torch/torchvision from the
# PyTorch CPU index so transformers/timm don't drag in the CUDA build.
set -euo pipefail
cd "$(dirname "$0")"

if [[ ! -x .venv/bin/pip ]]; then
  python3 -m venv --without-pip .venv
  curl -sSL https://bootstrap.pypa.io/get-pip.py | .venv/bin/python
fi

.venv/bin/pip install \
  torch==2.5.1+cpu torchvision==0.20.1+cpu \
  transformers einops timm pillow fastapi 'uvicorn[standard]' \
  --extra-index-url https://download.pytorch.org/whl/cpu

echo
echo "Vision sidecar ready. microsoft/Florence-2-base (~0.5 GB) downloads to the"
echo "HuggingFace cache on first run. Start with:"
echo "  .venv/bin/uvicorn server:app --host 127.0.0.1 --port 8791"
