"""rosshield PWA 아이콘 생성 스크립트입니다.

lucide-react ShieldCheck 아이콘과 일관된 단순 도형(방패 + 체크)을 PIL로
raster 합성합니다. SVG path 직접 변환은 PIL이 지원하지 않으므로 동등한
기하학적 표현으로 대체합니다.

산출:
    web/public/icon-192.png   (192x192 — manifest standard)
    web/public/icon-512.png   (512x512 — manifest standard)
    web/public/apple-touch-icon.png (180x180 — iOS)

실행:
    python web/scripts/generate-icons.py
"""

from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw

# 브랜드 컬러 — design doc §6.2 + theme_color 일관.
BG_COLOR = (10, 10, 10, 255)       # #0a0a0a — manifest theme_color
FG_COLOR = (255, 255, 255, 255)    # white — 방패 + 체크 stroke

PUBLIC_DIR = Path(__file__).resolve().parent.parent / "public"


def draw_shield_check(size: int) -> Image.Image:
    """단색 배경 + ShieldCheck 도형(방패 + 체크) 합성합니다.

    좌표 체계는 24x24 viewBox 기준을 size에 맞춰 비례 변환합니다.
    """

    img = Image.new("RGBA", (size, size), BG_COLOR)
    draw = ImageDraw.Draw(img)

    # viewBox 24x24 기준 → size 비율 변환.
    s = size / 24.0
    stroke = max(2, int(round(2.0 * s)))

    # 방패 외곽선 — lucide ShieldCheck path 근사 (다각형 stroke).
    # 좌표는 viewBox 24x24 안에서 추출 후 size로 스케일.
    shield_pts_24 = [
        (12.0, 2.4),    # 상단 중앙
        (5.0, 5.0),     # 좌상
        (4.0, 6.0),     # 좌측 시작
        (4.0, 13.0),    # 좌측 하강 시작
        (6.5, 18.5),    # 좌하 곡선 근사
        (12.0, 21.6),   # 하단 중앙
        (17.5, 18.5),   # 우하 곡선 근사
        (20.0, 13.0),   # 우측
        (20.0, 6.0),    # 우상
        (19.0, 5.0),    # 우상 시작
        (12.0, 2.4),    # 닫기
    ]
    shield_pts = [(x * s, y * s) for x, y in shield_pts_24]
    draw.line(shield_pts, fill=FG_COLOR, width=stroke, joint="curve")

    # 체크 마크 — "m9 12 2 2 4-4" → (9,12) → (11,14) → (15,10).
    check_pts_24 = [(9.0, 12.0), (11.0, 14.0), (15.0, 10.0)]
    check_pts = [(x * s, y * s) for x, y in check_pts_24]
    draw.line(check_pts, fill=FG_COLOR, width=stroke, joint="curve")

    return img


def main() -> None:
    PUBLIC_DIR.mkdir(parents=True, exist_ok=True)
    targets = {
        "icon-192.png": 192,
        "icon-512.png": 512,
        "apple-touch-icon.png": 180,
    }
    for name, size in targets.items():
        img = draw_shield_check(size)
        out = PUBLIC_DIR / name
        img.save(out, format="PNG", optimize=True)
        print(f"wrote {out} ({size}x{size})")


if __name__ == "__main__":
    main()
