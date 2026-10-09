"""Unit tests for the Gradio-compatible dashboard API."""
import sys
import pathlib
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))
import goradio as gr


class DashboardTests(unittest.TestCase):
    def make_interface(self, operation):
        return gr.Interface(
            fn=operation, inputs=gr.Textbox(label="Input"),
            outputs=gr.Textbox(label="Output"))

    def test_routes_register_independent_callbacks(self):
        home = self.make_interface(lambda value: "HOME-" + value)
        images = self.make_interface(lambda value: "IMAGE-" + value)
        d = gr.Dashboard(title="Admin")
        self.assertIs(d.route("Home", "/", home, icon="dashboard"), d)
        self.assertIs(d.route("Images", "/images", images, group="Tools"), d)
        self.assertEqual(d._dashboard_pages["home"], home)
        self.assertEqual(d._dashboard_pages["images"], images)
        self.assertEqual([item["path"] for item in d._dashboard_definitions], ["/", "/images"])
        self.assertEqual(d._dashboard_definitions[1]["group"], "Tools")
        self.assertEqual(d._dashboard_pages["home"].fn("test"), "HOME-test")
        self.assertEqual(d._dashboard_pages["images"].fn("test"), "IMAGE-test")

    def test_reserved_invalid_and_duplicate_paths(self):
        d = gr.Dashboard()
        d.route("Home", "/", self.make_interface(lambda x: x))
        with self.assertRaises(ValueError):
            d.route("Again", "/", self.make_interface(lambda x: x))
        for route in ["/auth", "/login", "/api", "/gradio_api", "/assets", "/static",
                      "/config", "/Wrong", "/a/child", "/../../escape"]:
            with self.subTest(path=route), self.assertRaises(ValueError):
                d.route("Invalid", route, self.make_interface(lambda x: x))
        with self.assertRaises(TypeError):
            d.route("Bad", "/bad", d)
        with self.assertRaises(ValueError):
            d.route("Missing", "/missing")

    def test_dashboard_initializes_as_regular_interface(self):
        d = gr.Dashboard()
        self.assertIsInstance(d, gr.Interface)
        self.assertEqual(d.title, "Goradio")
        self.assertEqual(d._dashboard_definitions, [])


if __name__ == "__main__":
    unittest.main()
