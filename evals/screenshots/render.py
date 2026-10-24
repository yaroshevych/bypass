"""Fixed-font rasterization of tmux SGR captures; no image comparison yet."""
import re
import unicodedata

from PIL import Image, ImageDraw, ImageFont

SGR = re.compile(r"\x1b\[([0-9;:]*)m")
ANSI = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")


def color256(n):
    base = [(0, 0, 0), (205, 0, 0), (0, 205, 0), (205, 205, 0),
            (0, 0, 238), (205, 0, 205), (0, 205, 205), (229, 229, 229),
            (127, 127, 127), (255, 0, 0), (0, 255, 0), (255, 255, 0),
            (92, 92, 255), (255, 0, 255), (0, 255, 255), (255, 255, 255)]
    if n < 16:
        return base[n]
    if n >= 232:
        return (8 + (n - 232) * 10,) * 3
    n -= 16
    levels = [0, 95, 135, 175, 215, 255]
    return levels[n // 36], levels[(n // 6) % 6], levels[n % 6]


def render(capture, destination, columns, rows, font_path, font_size, foreground, background):
    regular = ImageFont.truetype(str(font_path), font_size, index=0)
    bold = regular
    for index in range(4):
        try:
            face = ImageFont.truetype(str(font_path), font_size, index=index)
        except OSError:
            break  # Ordinary TTF files have one face; TTC collections have more.
        if face.getname()[1].lower() == 'bold':
            bold = face
    cw = max(1, round(regular.getlength('M')))
    ascent, descent = regular.getmetrics()
    ch = max(font_size + 6, ascent + descent)
    image = Image.new('RGB', (columns * cw, rows * ch), background)
    draw = ImageDraw.Draw(image)
    fg, bg, weight, underline, reverse, dim = foreground, background, False, False, False, False

    def style(raw):
        nonlocal fg, bg, weight, underline, reverse, dim
        values = [int(v) for v in raw.replace(':', ';').split(';') if v] or [0]
        i = 0
        while i < len(values):
            code = values[i]
            if code == 0:
                fg, bg, weight, underline, reverse, dim = foreground, background, False, False, False, False
            elif code == 1:
                weight = True
            elif code == 2:
                dim = True
            elif code == 22:
                weight = dim = False
            elif code == 4:
                underline = True
            elif code == 24:
                underline = False
            elif code == 7:
                reverse = True
            elif code == 27:
                reverse = False
            elif code == 39:
                fg = foreground
            elif code == 49:
                bg = background
            elif 30 <= code <= 37:
                fg = color256(code - 30)
            elif 90 <= code <= 97:
                fg = color256(code - 90 + 8)
            elif 40 <= code <= 47:
                bg = color256(code - 40)
            elif 100 <= code <= 107:
                bg = color256(code - 100 + 8)
            elif code in (38, 48) and i + 2 < len(values):
                value = None
                if values[i + 1] == 5:
                    value = color256(values[i + 2])
                    i += 2
                elif values[i + 1] == 2 and i + 4 < len(values):
                    value = tuple(values[i + 2:i + 5])
                    i += 4
                if value is not None:
                    if code == 38:
                        fg = value
                    else:
                        bg = value
            i += 1

    for row, line in enumerate(capture.splitlines()[:rows]):
        column = 0
        offset = 0
        while offset < len(line):
            match = SGR.match(line, offset)
            if match:
                style(match.group(1))
                offset = match.end()
                continue
            other = ANSI.match(line, offset)
            if other:
                offset = other.end()
                continue
            char = line[offset]
            offset += 1
            if unicodedata.category(char).startswith('C'):
                continue
            if unicodedata.combining(char):
                continue
            width = 2 if unicodedata.east_asian_width(char) in ('W', 'F') else 1
            if column >= columns:
                break
            ink, paper = (bg, fg) if reverse else (fg, bg)
            if dim:
                ink = tuple(round(a * .65 + b * .35) for a, b in zip(ink, paper))
            x, y = column * cw, row * ch
            draw.rectangle((x, y, x + cw * width - 1, y + ch - 1), fill=paper)
            baseline = y + (ch - ascent - descent) // 2 + ascent
            face = bold if weight else regular
            draw.text((x, baseline), char, font=face, fill=ink, anchor='ls')
            if underline:
                draw.line((x, y + ch - 3, x + cw * width - 1, y + ch - 3), fill=ink)
            column += width
        # tmux omits trailing blank cells but retains their active SGR state.
        # Paint that background through the pane edge (not the theme default).
        if column < columns:
            paper = fg if reverse else bg
            draw.rectangle((column * cw, row * ch, columns * cw - 1, (row + 1) * ch - 1), fill=paper)
    image.save(destination)

