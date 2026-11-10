"""Chatterbox voice-cloning worker for repi: loads the model once, then reads
one JSON request per line on stdin and answers one JSON line on stdout.

  request : {"id": 1, "text": "...", "voice": "/path/ref.wav", "speed": 1.0, "out": "/path.wav"}
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

MODEL_DIR = os.environ["REPI_CLONE_MODEL"]
EXAGGERATION = 0.5  # emotion; 0.5 is neutral, which suits listening practice
CFG_WEIGHT = 0.5
MAX_CHARS = 280  # longer text is spoken in pieces; Chatterbox drifts on long input


def device():
    if torch.cuda.is_available():
        return "cuda"
    if getattr(torch.backends, "mps", None) and torch.backends.mps.is_available():
        return "mps"
    return "cpu"


def pieces(text):
    """Split text at sentence ends, then commas, into pieces of at most MAX_CHARS."""
    out = []
    for sent in re.split(r"(?<=[.!?])\s+", text.strip()):
        while len(sent) > MAX_CHARS:
            cut = sent.rfind(",", 0, MAX_CHARS)
            if cut < MAX_CHARS // 3:
                cut = sent.rfind(" ", 0, MAX_CHARS)
            if cut <= 0:
                cut = MAX_CHARS
            out.append(sent[: cut + 1].strip())
            sent = sent[cut + 1 :].strip()
        if sent:
            out.append(sent)
    return out


def trim(audio, sr, pad=0.05, floor=0.01):
    """Cut leading and trailing near-silence, keeping a short pad."""
    loud = np.flatnonzero(np.abs(audio) > floor)
    if loud.size == 0:
        return audio
    p = int(sr * pad)
    return audio[max(loud[0] - p, 0) : loud[-1] + p]


def main():
    # Keep the protocol channel clean: libraries print banners to fd 1, so
    # move the real stdout aside and point fd 1 (and sys.stdout) at stderr.
    out = os.fdopen(os.dup(1), "w", buffering=1)
    os.dup2(2, 1)
    sys.stdout = sys.stderr
    from chatterbox.tts import ChatterboxTTS

    model = ChatterboxTTS.from_local(MODEL_DIR, device())
    sr = model.sr
    current = None  # reference wav the conditionals were made from

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
            if req["voice"] != current:
                model.prepare_conditionals(req["voice"], exaggeration=EXAGGERATION)
                current = req["voice"]
            chunks = []
            for i, p in enumerate(pieces(req["text"])):
                torch.manual_seed(i)  # the same sentence sounds the same every time
                wav = model.generate(p, exaggeration=EXAGGERATION, cfg_weight=CFG_WEIGHT)
                chunks.append(trim(wav.squeeze(0).cpu().numpy().astype(np.float32), sr))
                chunks.append(np.zeros(int(sr * 0.12), dtype=np.float32))
            if not chunks:
                raise ValueError("no audio produced")
            audio = np.concatenate(chunks[:-1])
            speed = req.get("speed") or 1.0
            if abs(speed - 1.0) > 0.01:  # Chatterbox has no speed control
                import librosa

                audio = librosa.effects.time_stretch(audio, rate=speed)
            peak = float(np.abs(audio).max())
            if peak > 0.95:
                audio = audio * (0.95 / peak)
            tmp = req["out"] + ".tmp.wav"
            sf.write(tmp, audio, sr, subtype="PCM_16")
            os.replace(tmp, req["out"])
            reply({"id": req["id"], "ok": True})
        except Exception as e:  # keep serving after one bad sentence
            reply({"id": req.get("id"), "ok": False, "error": f"{type(e).__name__}: {e}"})


if __name__ == "__main__":
    main()
