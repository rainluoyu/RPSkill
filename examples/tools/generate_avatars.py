"""Deterministically draw small geometric PNG avatars; no AI/image service.

Run: python3 examples/tools/generate_avatars.py
Only the four documented example PNG assets are replaced.
"""

from pathlib import Path
import struct
import zlib


WORKSPACE = Path(__file__).resolve().parents[1] / "harbor-academy"
SIZE = 96


def chunk(kind: bytes, payload: bytes) -> bytes:
    return (
        struct.pack(">I", len(payload))
        + kind
        + payload
        + struct.pack(">I", zlib.crc32(kind + payload) & 0xFFFFFFFF)
    )


def draw(character: str, expression: str = "neutral") -> bytes:
    background = (222, 235, 240) if character == "alice" else (245, 233, 209)
    coat = (65, 101, 127) if character == "alice" else (170, 121, 64)
    hair = (48, 59, 69) if character == "alice" else (101, 70, 48)
    skin = (231, 189, 153)
    ink = (44, 45, 48)
    pixels = [list(background) for _ in range(SIZE * SIZE)]

    def set_pixel(x, y, color):
        if 0 <= x < SIZE and 0 <= y < SIZE:
            pixels[y * SIZE + x] = list(color)

    def ellipse(cx, cy, rx, ry, color):
        for y in range(cy - ry, cy + ry + 1):
            for x in range(cx - rx, cx + rx + 1):
                if ((x - cx) * ry) ** 2 + ((y - cy) * rx) ** 2 <= (rx * ry) ** 2:
                    set_pixel(x, y, color)

    def line(x0, y0, x1, y1, color):
        steps = max(abs(x1 - x0), abs(y1 - y0), 1)
        for i in range(steps + 1):
            set_pixel(round(x0 + (x1 - x0) * i / steps),
                      round(y0 + (y1 - y0) * i / steps), color)

    ellipse(48, 92, 36, 28, coat)
    ellipse(48, 62, 8, 13, skin)
    ellipse(48, 38, 24, 28, hair)
    ellipse(48, 43, 19, 23, skin)
    ellipse(45, 23, 20, 10, hair)
    if character == "alice":
        ellipse(30, 36, 5, 18, hair)
        ellipse(65, 36, 4, 16, hair)
    else:
        for x in (31, 39, 49, 58, 64):
            ellipse(x, 19, 7, 6, hair)
        for x in (39, 57):
            ellipse(x, 43, 8, 7, ink)
            ellipse(x, 43, 6, 5, skin)
        line(47, 43, 49, 43, ink)
    ellipse(39, 43, 1, 2, ink)
    ellipse(57, 43, 1, 2, ink)
    if expression == "focused":
        line(34, 37, 42, 39, ink)
        line(54, 39, 62, 37, ink)
    else:
        line(34, 36, 42, 36, hair)
        line(54, 36, 62, 36, hair)
    if expression == "smile":
        line(41, 55, 44, 58, ink)
        line(44, 58, 52, 58, ink)
        line(52, 58, 55, 55, ink)
    else:
        line(43, 56, 53, 56, ink)
    line(48, 47, 46, 51, (191, 143, 108))
    line(34, 72, 48, 84, background)
    line(48, 84, 62, 72, background)
    raw = b"".join(
        b"\x00" + bytes(component for pixel in pixels[y * SIZE:(y + 1) * SIZE]
                          for component in pixel)
        for y in range(SIZE)
    )
    return (b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", SIZE, SIZE, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9))
            + chunk(b"IEND", b""))


def assets():
    return {
        "characters/alice/avatar.png": draw("alice"),
        "characters/alice/expressions/focused.png": draw("alice", "focused"),
        "characters/bob/avatar.png": draw("bob"),
        "characters/bob/expressions/smile.png": draw("bob", "smile"),
    }


if __name__ == "__main__":
    for relative, data in assets().items():
        target = WORKSPACE / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        print(f"{relative}: {len(data)} bytes")
