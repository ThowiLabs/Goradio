import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import goradio as gr


class ComponentTests(unittest.TestCase):
    def test_slider_validation(self):
        with self.assertRaises(ValueError):
            gr.Slider(minimum=8, maximum=2)

    def test_interface_accepts_strings(self):
        app = gr.Interface(str, inputs="textbox", outputs="textbox")
        self.assertEqual(app.inputs[0].type, "textbox")

    def test_invalid_component(self):
        with self.assertRaises(ValueError):
            gr.Component("unknown")

    def test_media_requires_started_interface(self):
        with self.assertRaises(RuntimeError):
            gr.media("unknown.png")

    def test_dropdown_choices(self):
        self.assertEqual(gr.Dropdown(["A", "B"]).config()["choices"], ["A", "B"])



    def test_empty_auth_disables_basic(self):
        self.assertIsNone(gr._normalize_auth(None))
        self.assertIsNone(gr._normalize_auth(("", "")))
        self.assertIsNone(gr._normalize_auth(("  ", "  ")))

    def test_four_digit_pin_accepted(self):
        self.assertEqual(gr._normalize_auth(("admin", "1234")), ("admin", "1234"))

    def test_incomplete_or_short_auth_rejected(self):
        for credentials in [("admin", ""), ("", "1234"), ("admin", "123"), ("admin", "   ")]:
            with self.subTest(auth=credentials), self.assertRaises(ValueError):
                gr._normalize_auth(credentials)

if __name__ == "__main__":
    unittest.main()
