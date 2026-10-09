import unittest
import goradio as gr

class ReactiveTests(unittest.TestCase):
    def test_live_timer_and_generator(self):
        def fn(text):
            yield text + " 1"
            yield text + " 2"
        app=gr.Interface(fn=fn,inputs=gr.Textbox(),outputs=gr.Textbox(),
                         live=True,load=True,every=2,validate=lambda x: None)
        self.assertTrue(app.live)
        self.assertTrue(app.load)
        self.assertEqual(app.every,2)
        self.assertTrue(app.streaming)
        self.assertIsNotNone(app.validator)

    def test_invalid_events_and_inputs(self):
        for value in (-1,.1,3601,float('inf')):
            with self.subTest(value=value), self.assertRaises(ValueError):
                gr.Interface(fn=lambda x:x,inputs=gr.Textbox(),outputs=gr.Textbox(),every=value)
        with self.assertRaises(TypeError):
            gr.Interface(fn=lambda x:x,inputs=gr.Textbox(),outputs=gr.Textbox(),validate="bad")
        with self.assertRaises(ValueError):
            gr.Textbox(min_length=5,max_length=3)
        with self.assertRaises(ValueError):
            gr.Textbox(pattern="(")

    def test_update_patch_contract(self):
        self.assertEqual(gr.update("hello",visible=False),{"__type__":"update","value":"hello","visible":False})
        self.assertEqual(gr.update(interactive=True),{"__type__":"update","interactive":True})

    def test_session_state_is_isolated(self):
        from goradio import _ACTIVE_SESSION
        class Session:
            import threading
            _session_lock=threading.Lock()
            _session_state={}
        app=Session()
        a=_ACTIVE_SESSION.set((app,"abcdefghijkl"))
        try:
            gr.state_set("count",4)
            self.assertEqual(gr.state_get("count"),4)
        finally:
            _ACTIVE_SESSION.reset(a)
        b=_ACTIVE_SESSION.set((app,"zyxwvutsrqp"))
        try:
            self.assertIsNone(gr.state_get("count"))
        finally:
            _ACTIVE_SESSION.reset(b)
        with self.assertRaises(RuntimeError):
            gr.state_get("count")
