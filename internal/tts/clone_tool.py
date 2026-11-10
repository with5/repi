"""Reference-voice tools for repi's Chatterbox engine. Each command prints one
JSON line on stdout: {"ok": true, "seconds": 9.4} or {"ok": false, "error": "..."}.

  prep SRC DST  any audio file -> mono 24 kHz WAV, silence trimmed, loudness
                evened, at most MAX_SECONDS long.
  record DST    record the default microphone. Prints {"recording": true} once
                it hears, and stops at a line on stdin, at EOF or after
                LIMIT_SECONDS.
"""
import json
import sys
import threading

import numpy as np
import soundfile as sf

SR = 24000
MIN_SECONDS = 3  # less than this and the voice is not learned
MAX_SECONDS = 15  # Chatterbox uses at most the first ~10 s
LIMIT_SECONDS = 30


def say(obj):
    sys.__stdout__.write(json.dumps(obj, ensure_ascii=False) + "\n")
    sys.__stdout__.flush()


def prep(src, dst):
    import librosa
    import pyloudnorm

    try:
        y, _ = librosa.load(src, sr=SR, mono=True)
    except Exception as e:
        raise ValueError(f"读不了这个音频文件（{type(e).__name__}），请换成 WAV、FLAC、MP3 或 OGG") from e
    y, _ = librosa.effects.trim(y, top_db=40)
    seconds = len(y) / SR
    if seconds < MIN_SECONDS:
        raise ValueError(f"有效声音只有 {seconds:.1f} 秒，至少要 {MIN_SECONDS} 秒，建议 8–15 秒")
    y = y[: MAX_SECONDS * SR]
    loud = pyloudnorm.Meter(SR).integrated_loudness(y)
    if np.isfinite(loud):
        y = pyloudnorm.normalize.loudness(y, loud, -20.0)
    peak = float(np.abs(y).max())
    if peak > 0.95:
        y = y * (0.95 / peak)
    sf.write(dst, y.astype(np.float32), SR, subtype="PCM_16")
    return len(y) / SR


def record(dst):
    try:
        import sounddevice as sd

        rate = int(sd.query_devices(kind="input")["default_samplerate"])
    except Exception as e:
        raise ValueError(f"打不开麦克风：{e}。也可以用“导入音频”") from e
    frames = []
    stop = threading.Event()
    threading.Thread(target=lambda: (sys.stdin.readline(), stop.set()), daemon=True).start()
    with sd.InputStream(samplerate=rate, channels=1, dtype="float32", callback=lambda d, *_: frames.append(d.copy())):
        say({"recording": True})
        stop.wait(LIMIT_SECONDS)
    if not frames:
        raise ValueError("没有录到声音，请检查麦克风")
    audio = np.concatenate(frames)[:, 0]
    if float(np.abs(audio).max()) < 0.01:
        raise ValueError("录到的几乎是静音，请检查麦克风是否被静音或选错了设备")
    sf.write(dst, audio, rate, subtype="PCM_16")
    return len(audio) / rate


def main():
    sys.stdout = sys.stderr  # keep stdout for our JSON lines
    cmd, args = sys.argv[1], sys.argv[2:]
    try:
        seconds = prep(*args) if cmd == "prep" else record(*args)
        say({"ok": True, "seconds": round(seconds, 2)})
    except Exception as e:
        say({"ok": False, "error": str(e) if isinstance(e, ValueError) else f"{type(e).__name__}: {e}"})


if __name__ == "__main__":
    main()
