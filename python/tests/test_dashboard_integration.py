"""Opt-in end-to-end test of Python -> Go multipage callbacks.

GORADIO_RUN_INTEGRATION=1 python -m unittest discover -s python/tests -p 'test_dashboard_integration.py' -v
A compiled Go executable should be specified with GORADIO_BINARY.
"""
import json
import os
import pathlib
import socket
import sys
import unittest
from urllib.request import Request, urlopen

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))
import goradio as gr


@unittest.skipUnless(os.getenv("GORADIO_RUN_INTEGRATION") == "1",
                     "set GORADIO_RUN_INTEGRATION=1 for browser/backend integration")
class DashboardIntegrationTests(unittest.TestCase):
    def test_each_python_callback_is_routed_to_its_window(self):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]

        d = gr.Dashboard(title="Python Callback QA")
        d.route("Inicio", "/", gr.Interface(
            lambda value: "Inicio:" + value,
            gr.Textbox(label="Input"), gr.Textbox(label="Output")), icon="dashboard")
        d.route("Herramientas", "/herramientas", gr.Interface(
            lambda value: "Tools:" + value.upper(),
            gr.Textbox(label="Input"), gr.Textbox(label="Output")), icon="file")
        d.launch(server_port=port, share=False, block=False)
        try:
            for path, expected in (
                ("/api/predict", "Inicio:hello"),
                ("/herramientas/api/predict", "Tools:HELLO"),
            ):
                payload = json.dumps({"data": ["hello"]}).encode("utf-8")
                request = Request("http://127.0.0.1:" + str(port) + path,
                                  data=payload, method="POST",
                                  headers={"Content-Type": "application/json"})
                with urlopen(request, timeout=15) as response:
                    self.assertEqual(response.status, 200)
                    body = json.load(response)
                self.assertEqual(body.get("data"), [expected], path)

            with urlopen("http://127.0.0.1:" + str(port) + "/herramientas",
                         timeout=8) as response:
                self.assertEqual(response.status, 200)
                body = response.read().decode("utf-8")
                self.assertIn("gd-sidebar", body)
        finally:
            d.close()


if __name__ == "__main__":
    unittest.main()
