import unittest
import goradio as gr

class ChatInterfaceTests(unittest.TestCase):
    def test_chat_interface_structure(self):
        def respond(message,history):
            yield "Procesando"
            yield "Respuesta: "+message
        app=gr.ChatInterface(respond,title="Chat original")
        self.assertTrue(app.streaming)
        self.assertEqual(app.layout.type,"column")
        self.assertEqual(app.inputs[0].key,"message")
        self.assertEqual(app.outputs[0].type,"chatbot")
        self.assertEqual(app.outputs[0].key,"conversation")
        with self.assertRaises(ValueError):
            gr.ChatInterface(respond,max_history=0)

    def test_chat_history_is_session_isolated(self):
        from goradio import _ACTIVE_SESSION
        import inspect
        app=gr.ChatInterface(lambda m,h: f"{len(h)}:{m}")
        a=_ACTIVE_SESSION.set((app,"aaaaaa11111111"))
        try:
            first=list(app.fn("Hola"))[-1]
            second=list(app.fn("Otra"))[-1]
            self.assertEqual(first[-1]["content"],"0:Hola")
            self.assertEqual(second[-1]["content"],"2:Otra")
        finally:
            _ACTIVE_SESSION.reset(a)
        b=_ACTIVE_SESSION.set((app,"bbbbbb22222222"))
        try:
            third=list(app.fn("Hola"))[-1]
            self.assertEqual(third[-1]["content"],"0:Hola")
        finally:
            _ACTIVE_SESSION.reset(b)
