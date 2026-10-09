"""Fixture multimedia autocontenida para auditar componentes Gradio."""
from __future__ import annotations

from pathlib import Path
from math import sin, pi
import struct
import subprocess
import wave

root = Path(__file__).resolve().parent / "_fixtures"
root.mkdir(parents=True, exist_ok=True)

audio_path = root / "tone.wav"
with wave.open(str(audio_path), "wb") as audio:
    audio.setnchannels(1)
    audio.setsampwidth(2)
    audio.setframerate(16000)
    samples = (int(7000 * sin(2 * pi * 440 * i / 16000)) for i in range(4000))
    audio.writeframes(b"".join(struct.pack("<h", v) for v in samples))

video_path = root / "tiny.mp4"
command = [
    "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
    "-f", "lavfi", "-i", "color=c=blue:s=160x120:r=10:d=0.6",
    "-c:v", "libx264", "-preset", "ultrafast", "-crf", "30", "-pix_fmt", "yuv420p", "-movflags", "+faststart",
    str(video_path),
]
subprocess.run(command, check=True, timeout=30)
print(f"audio={audio_path} ({audio_path.stat().st_size} bytes)")
print(f"video={video_path} ({video_path.stat().st_size} bytes)")
