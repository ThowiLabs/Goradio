import inspect
import unittest
import goradio as gr

class BlocksStreamTests(unittest.TestCase):
    def test_mixed_streaming_and_standard_callbacks(self):
        def progressive(value):
            yield value + "-1"
            yield value + "-2"
        layout=gr.Row(gr.Ref("query"),gr.Button("stream","Progress"),
                      gr.Button("instant","Instant"),gr.Ref("progress"),gr.Ref("result"))
        app=gr.Blocks(fn=lambda value:(value,value),
            inputs=[gr.Textbox(key="query")],
            outputs=[gr.Markdown(key="progress"),gr.Textbox(key="result")],
            layout=layout)
        app.on("stream","click",progressive,inputs=["query"],outputs=["progress"])
        app.on("instant","click",lambda s:s.upper(),inputs=["query"],outputs=["result"])
        self.assertTrue(app._events[0][0]["streaming"])
        self.assertFalse(app._events[1][0]["streaming"])
        self.assertTrue(inspect.isgeneratorfunction(app._events[0][1]))
        self.assertEqual(list(app._events[0][1]("x")),["x-1","x-2"])
    def test_rejects_invalid_event(self):
        app=gr.Blocks(fn=lambda q:q,
            inputs=[gr.Textbox(key="query")],
            outputs=[gr.Textbox(key="result")],
            layout=gr.Row(gr.Ref("query"),gr.Button("btn","Run"),gr.Ref("result")))
        def generator(q):
            yield q
        with self.assertRaises(ValueError):
            app.on("btn","change",generator,inputs=["query"],outputs=["result"])
        app.on("btn","click",generator,inputs=["query"],outputs=["result"])
        with self.assertRaises(ValueError):
            app.on("btn","click",lambda q:q,inputs=["query"],outputs=["result"])

if __name__=="__main__":unittest.main()
