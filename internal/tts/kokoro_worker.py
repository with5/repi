"""Kokoro-82M worker for repi: loads the model once, then reads one JSON request
per line on stdin and answers one JSON line on stdout.

  request : {"id": 1, "text": "...", "voice": "af_heart", "speed": 1.0, "out": "/path.wav"}
  response: {"id": 1, "ok": true} | {"id": 1, "ok": false, "error": "..."}
First line written is {"ready": true} once the model is loaded.
Everything else (progress, warnings) goes to stderr.
"""
import json
import os
import sys

import numpy as np
import soundfile as sf
import torch
from kokoro import KModel, KPipeline

REPO = os.environ.get("REPI_KOKORO_REPO", "hexgrad/Kokoro-82M")
SAMPLE_RATE = 24000
DEVICE = "cuda" if torch.cuda.is_available() else "cpu"


def main():
    # Keep the protocol channel clean: libraries print banners to fd 1, so
    # move the real stdout aside and point fd 1 (and sys.stdout) at stderr.
    out = os.fdopen(os.dup(1), "w", buffering=1)
    os.dup2(2, 1)
    sys.stdout = sys.stderr
    model = KModel(repo_id=REPO).to(DEVICE).eval()
    pipelines = {}  # lang code ("a" American, "b" British) -> pipeline

    def pipeline(voice):
        lang = voice[0]
        if lang not in pipelines:
            pipelines[lang] = KPipeline(lang_code=lang, repo_id=REPO, model=model)
        return pipelines[lang]

    def reply(obj):
        out.write(json.dumps(obj, ensure_ascii=False) + "\n")
        out.flush()

    reply({"ready": True})
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        req = json.loads(line)
        try:
            chunks = []
            with torch.inference_mode():
                for r in pipeline(req["voice"])(req["text"], voice=req["voice"], speed=req.get("speed") or 1.0):
                    if r.audio is not None:
                        chunks.append(r.audio.cpu().numpy())
            if not chunks:
                raise ValueError("no audio produced")
            tmp = req["out"] + ".tmp.wav"
            sf.write(tmp, np.concatenate(chunks), SAMPLE_RATE)
            os.replace(tmp, req["out"])
            reply({"id": req["id"], "ok": True})
        except Exception as e:  # keep serving after one bad sentence
            reply({"id": req.get("id"), "ok": False, "error": f"{type(e).__name__}: {e}"})


if __name__ == "__main__":
    main()
