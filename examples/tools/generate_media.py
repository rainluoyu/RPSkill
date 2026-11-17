"""Create tiny synthetic demo media locally; no model or external service.

python3 examples/tools/generate_media.py
Requires ffmpeg only when regenerating the shipped MP4; playback/import does not.
Outputs examples/assets/tone.wav and signal.mp4, never a running Workspace.
"""
import math
from pathlib import Path
import struct
import subprocess
import wave

ASSETS = Path(__file__).resolve().parents[1] / "assets"


def tone(path):
    with wave.open(str(path), "wb") as output:
        output.setparams((1, 2, 8000, 0, "NONE", "not compressed"))
        output.writeframes(b"".join(struct.pack("<h", round(1200 * math.sin(2 * math.pi * 440 * n / 8000)))
                                    for n in range(2000)))


if __name__ == "__main__":
    ASSETS.mkdir(parents=True, exist_ok=True)
    tone(ASSETS / "tone.wav")
    subprocess.run(["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi",
                    "-i", "color=c=0x41657f:s=96x96:r=4:d=1", "-an", "-c:v", "libx264",
                    "-pix_fmt", "yuv420p", "-map_metadata", "-1", "-movflags", "+faststart",
                    str(ASSETS / "signal.mp4")], check=True)
