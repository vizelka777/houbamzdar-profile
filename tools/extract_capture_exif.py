#!/usr/bin/env python3

import json
import sys
from datetime import datetime, timezone

from PIL import Image, ImageOps


def gps_to_degrees(values):
    return float(values[0]) + float(values[1]) / 60.0 + float(values[2]) / 3600.0


def parse_gps(gps_info):
    if not gps_info:
        return None, None

    lat_values = gps_info.get(2)
    lat_ref = gps_info.get(1)
    lon_values = gps_info.get(4)
    lon_ref = gps_info.get(3)
    if not lat_values or not lat_ref or not lon_values or not lon_ref:
        return None, None

    latitude = gps_to_degrees(lat_values)
    longitude = gps_to_degrees(lon_values)

    if str(lat_ref).upper() == "S":
        latitude = -latitude
    if str(lon_ref).upper() == "W":
        longitude = -longitude

    return latitude, longitude


def parse_captured_at(exif):
    raw_value = exif.get(36867) or exif.get(36868) or exif.get(306)
    if not raw_value:
        return ""

    try:
        parsed = datetime.strptime(str(raw_value), "%Y:%m:%d %H:%M:%S")
    except ValueError:
        return ""
    return parsed.replace(tzinfo=timezone.utc).isoformat().replace("+00:00", "Z")


def main() -> int:
    if len(sys.argv) != 2:
        print(json.dumps({"ok": False, "reason": "invalid_arguments"}))
        return 0

    path = sys.argv[1]

    try:
        with Image.open(path) as img:
            # Mirror browser upload flow: force EXIF load after orientation-aware open.
            ImageOps.exif_transpose(img)
            exif = img.getexif()
    except Exception:
        print(json.dumps({"ok": False, "reason": "missing_exif"}))
        return 0

    if not exif:
        print(json.dumps({"ok": False, "reason": "missing_exif"}))
        return 0

    try:
        gps_info = exif.get_ifd(34853)
    except Exception:
        gps_info = None
    latitude, longitude = parse_gps(gps_info)
    if latitude is None or longitude is None:
        print(json.dumps({"ok": False, "reason": "missing_gps"}))
        return 0

    print(
        json.dumps(
            {
                "ok": True,
                "latitude": latitude,
                "longitude": longitude,
                "captured_at": parse_captured_at(exif),
            }
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
