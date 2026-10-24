"""Renderer correctness checks; deliberately no approved-image comparisons."""
from pathlib import Path
import tempfile
import unittest

from PIL import Image
from render import render

FONT = Path('/System/Library/Fonts/Menlo.ttc')


@unittest.skipUnless(FONT.exists(), 'requires local Menlo font')
class RenderTests(unittest.TestCase):
    def test_tmux_trailing_cells_keep_active_background(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'screen.png'
            render('\x1b[48;2;10;20;30mX\n\x1b[49m', path, 10, 2, FONT, 20,
                   (255, 255, 255), (1, 2, 3))
            with Image.open(path) as image:
                self.assertEqual(image.size[0] // 10 * 10, image.size[0])
                self.assertEqual(image.getpixel((image.width-1, 0)), (10, 20, 30))
                self.assertEqual(image.getpixel((image.width-1, image.height-1)), (1, 2, 3))

    def test_reverse_video_and_256_color_background(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'screen.png'
            render('\x1b[38;5;196;7m ', path, 2, 1, FONT, 20,
                   (255, 255, 255), (0, 0, 0))
            with Image.open(path) as image:
                self.assertEqual(image.getpixel((0, 0)), (255, 0, 0))
                self.assertEqual(image.getpixel((image.width-1, 0)), (255, 0, 0))


if __name__ == '__main__':
    unittest.main()
