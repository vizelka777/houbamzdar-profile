#!/usr/bin/env python3

import json
import math
import sys

from PIL import Image, ImageOps

# Optional HEIC/HEIF support is enabled when pillow-heif is available in PYTHONPATH.
try:
    from pillow_heif import register_heif_opener
except Exception:  # pragma: no cover - optional dependency
    register_heif_opener = None

if register_heif_opener is not None:
    register_heif_opener()


def main() -> int:
    if len(sys.argv) != 5:
        print(json.dumps({"ok": False, "reason": "invalid_arguments"}))
        return 0

    source_path = sys.argv[1]
    output_path = sys.argv[2]
    max_dimension = int(sys.argv[3])
    quality = int(sys.argv[4])

    try:
        with Image.open(source_path) as img:
            image = ImageOps.exif_transpose(img)
            if image.mode not in ("RGB", "L"):
                image = image.convert("RGB")
            elif image.mode == "L":
                image = image.convert("RGB")

            width, height = image.size
            if width > max_dimension or height > max_dimension:
                ratio = min(max_dimension / width, max_dimension / height)
                next_width = max(1, int(math.floor(width * ratio + 0.5)))
                next_height = max(1, int(math.floor(height * ratio + 0.5)))
                image = image.resize((next_width, next_height), Image.Resampling.LANCZOS)

            width, height = image.size
            image.save(output_path, format="JPEG", quality=quality, optimize=True)
    except Exception as exc:
        print(json.dumps({"ok": False, "reason": f"normalize_failed:{type(exc).__name__}"}))
        return 0

    print(
        json.dumps(
            {
                "ok": True,
                "content_type": "image/jpeg",
                "width": width,
                "height": height,
            }
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
