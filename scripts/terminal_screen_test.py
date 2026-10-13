import unittest

from terminal_screen import screen


class ScreenResizeTests(unittest.TestCase):
    def test_resize_preserves_previous_wrapping(self):
        prefix = b'abcdefgh'
        raw = prefix + b'\r\x1b[BZ'
        rendered = screen(raw, 8, 4, [(0, 4, 3), (len(prefix), 8, 4)])
        self.assertEqual(rendered.splitlines(), ['abcd    ', 'efgh    ', 'Z       ', '        '])

    def test_resize_offsets_are_bytes_with_unicode_and_colour(self):
        prefix = '\x1b[31m界ab\x1b[0mc'.encode()
        raw = prefix + b'\r\x1b[BZ'
        rendered = screen(raw, 8, 4, [(0, 4, 3), (len(prefix), 8, 4)])
        self.assertEqual(rendered.splitlines(), ['界 ab    ', 'c       ', 'Z       ', '        '])

    def test_resize_resets_full_screen_scroll_region(self):
        prefix = b'\x1b[2;1Hbottom'
        raw = prefix + b'\x1b[3;1Hthird\r\nfourth'
        rendered = screen(raw, 8, 4, [(0, 8, 2), (len(prefix), 8, 4)])
        self.assertEqual(rendered.splitlines(), ['        ', 'bottom  ', 'third   ', 'fourth  '])

    def test_resize_without_new_output(self):
        raw = b'hi'
        self.assertEqual(screen(raw, 6, 2, [(0, 4, 1), (2, 6, 2)]), 'hi    \n      ')


if __name__ == '__main__':
    unittest.main()
