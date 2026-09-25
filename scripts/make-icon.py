"""Draw the Keystone mark: black stone, MojoSoMint mint outline."""

from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "assets"
MINT = (123, 255, 236, 255)
BLACK = (13, 13, 13, 255)


def shape(size: int) -> Image.Image:
    scale = size / 64
    image = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    fill = Image.new("L", (size, size), 0)
    draw_fill = ImageDraw.Draw(fill)

    def xy(points):
        return [(x * scale, y * scale) for x, y in points]

    outer = xy([(8, 50), (18, 8), (46, 8), (56, 50)])
    door = xy([(26, 50), (26, 26), (38, 26), (38, 50)])
    draw_fill.polygon(outer, fill=255)
    draw_fill.polygon(door, fill=0)
    black = Image.new("RGBA", (size, size), BLACK)
    image.paste(black, mask=fill)

    stroke = ImageDraw.Draw(image)
    width = max(2, round(size * 2.6 / 64))
    stroke.line(outer + [outer[0]], fill=MINT, width=width, joint="curve")
    stroke.line(door + [door[0]], fill=MINT, width=width, joint="curve")
    return image


def main() -> None:
    OUT.mkdir(exist_ok=True)
    master = shape(512)
    master.save(OUT / "keystone.png")
    sizes = [16, 24, 32, 48, 64, 256]
    icons = [shape(size) for size in sizes]
    icons[-1].save(OUT / "keystone.ico", sizes=[(size, size) for size in sizes], append_images=icons[:-1])


if __name__ == "__main__":
    main()
