#!/usr/bin/env python3
from __future__ import annotations

import argparse
from pathlib import Path

import numpy as np
import soundfile as sf
from kokoro import KPipeline


def language_from_voice(voice: str) -> str:
    voice = (voice or "").strip()
    if not voice:
        return "z"
    return voice[0].lower()


def to_numpy(audio):
    if hasattr(audio, "detach"):
        return audio.detach().cpu().numpy()
    return np.asarray(audio, dtype=np.float32)


def main() -> None:
    parser = argparse.ArgumentParser(description="Generate a WAV file with Kokoro.")
    parser.add_argument("--text", default="")
    parser.add_argument("--text-file", default="")
    parser.add_argument("--voice", default="zf_xiaoyi")
    parser.add_argument("--speed", type=float, default=1.0)
    parser.add_argument("--output", required=True)
    parser.add_argument("--lang", default="")
    parser.add_argument("--device", default="cpu")
    args = parser.parse_args()

    text = args.text
    if args.text_file:
        text = Path(args.text_file).read_text(encoding="utf-8")
    text = text.strip()
    if not text:
        text = " "

    voice = args.voice.strip() or "zf_xiaoyi"
    lang = args.lang.strip() or language_from_voice(voice)
    speed = min(2.0, max(0.1, args.speed))

    pipeline = KPipeline(lang_code=lang, repo_id="hexgrad/Kokoro-82M", device=args.device)
    chunks = []
    for segment in pipeline(text, voice=voice, speed=speed):
        audio = getattr(segment, "audio", None)
        if audio is None and isinstance(segment, (tuple, list)) and segment:
            audio = segment[-1]
        if audio is not None:
            chunks.append(to_numpy(audio))

    if not chunks:
        raise RuntimeError("Kokoro returned no audio segments")

    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    sf.write(output, np.concatenate(chunks), 24000)


if __name__ == "__main__":
    main()
