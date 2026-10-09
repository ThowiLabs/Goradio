import unittest
import goradio as gr

class ThemeTests(unittest.TestCase):
    def test_theme_accepts_hex_colors(self):
        custom = gr.Theme(primary_color="#8654cc", secondary_color="#295d9e")
        self.assertEqual(custom.primary_color, "#8654cc")
        app = gr.Interface(fn=lambda value: value, inputs=gr.Textbox(), outputs=gr.Textbox(), theme=custom)
        self.assertIs(app.theme, custom)
        dash = gr.Dashboard(theme=custom)
        self.assertIs(dash.theme, custom)

    def test_bad_theme_rejected(self):
        for color in ("orange", "#abc", "url(javascript:alert(1))"):
            with self.subTest(color=color), self.assertRaises(ValueError):
                gr.Theme(primary_color=color)
        with self.assertRaises(TypeError):
            gr.Interface(fn=lambda v: v,inputs=gr.Textbox(),outputs=gr.Textbox(),theme="#ff0000")
