"""Kokoro-82M worker for repi: loads the model once, then reads one JSON request
per line on stdin and answers one JSON line on stdout.

  request : {"id": 1, "text": "...", "voice": "af_heart", "speed": 1.0, "out": "/path.wav"}
  response: {"id": 1, "ok": true} | {"id": 1, "ok": false, "error": "..."}
First line written is {"ready": true} once the model is loaded.
Everything else (progress, warnings) goes to stderr.
"""
import json
import os
import re
import sys

import numpy as np
import soundfile as sf
import torch
from kokoro import KModel, KPipeline

REPO = os.environ.get("REPI_KOKORO_REPO", "hexgrad/Kokoro-82M")
SAMPLE_RATE = 24000
DEVICE = "cuda" if torch.cuda.is_available() else "cpu"
MODEL_DIR = os.environ.get("REPI_KOKORO_MODEL", "")


def local(name):
    """Path of a model file in MODEL_DIR, or None."""
    if not MODEL_DIR:
        return None
    p = os.path.join(MODEL_DIR, *name.split("/"))
    return p if os.path.exists(p) else None


# A run of English words inside Chinese text.
LATIN = re.compile(r"[A-Za-z][A-Za-z'’-]*(?:\s+[A-Za-z][A-Za-z'’-]*)*")


def mixed_g2p(zh, en):
    """Wraps Kokoro's Chinese G2P, which (with the v1.0 model) passes English
    words through as letters: English runs get English phonemes instead.
    en returns the English G2P, made the first time it is needed."""

    def g2p(text):
        out, pos = [], 0
        for m in LATIN.finditer(text):
            if m.start() > pos:
                out.append(zh(text[pos:m.start()])[0])
            out.append(" " + en()(m.group())[0] + " ")
            pos = m.end()
        if pos < len(text):
            out.append(zh(text[pos:])[0])
        ps = re.sub(r" +([,.!?;:])", r"\1", re.sub(r" +", " ", "".join(out)))
        return ps.strip(), None

    return g2p


def main():
    # Keep the protocol channel clean: libraries print banners to fd 1, so
    # move the real stdout aside and point fd 1 (and sys.stdout) at stderr.
    out = os.fdopen(os.dup(1), "w", buffering=1)
    os.dup2(2, 1)
    sys.stdout = sys.stderr
    cfg, pth = local("config.json"), local("kokoro-v1_0.pth")
    if cfg and pth:
        model = KModel(repo_id=REPO, config=cfg, model=pth)
    else:
        model = KModel(repo_id=REPO)
    model = model.to(DEVICE).eval()
    pipelines = {}  # lang code ("a" American, "b" British, "z" Chinese) -> pipeline

    def pipeline(voice):
        lang = voice[0]
        if lang not in pipelines:
            p = KPipeline(lang_code=lang, repo_id=REPO, model=model)
            if lang == "z":
                p.g2p = mixed_g2p(p.g2p, lambda: pipeline("a").g2p)
            pipelines[lang] = p
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
                voice = req["voice"]
                for r in pipeline(voice)(req["text"], voice=local(f"voices/{voice}.pt") or voice, speed=req.get("speed") or 1.0):
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
