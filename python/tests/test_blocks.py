import unittest
import goradio as gr

class BlocksTests(unittest.TestCase):
    def create(self):
        query = gr.Textbox(key="query", label="Consulta")
        result = gr.Markdown(key="result", label="Resultado")
        table = gr.Dataframe(key="table", label="Tabla", headers=["Campo", "Valor"])
        layout = gr.Tabs(
            gr.Tab("Datos", gr.Row(gr.Ref("query"), gr.Button("run", "Ejecutar"))),
            gr.Tab("Resultados", gr.Column(gr.Ref("result"), gr.Ref("table"))),
        )
        demo=gr.Blocks(fn=lambda x: [x, {"headers":["Campo","Valor"], "data":[["X",x]]}],
                       inputs=[query], outputs=[result,table],layout=layout)
        return demo

    def test_blocks_events(self):
        app=self.create()
        self.assertEqual(app.layout.type,"tabs")
        app.on("run","click",lambda x:x.upper(),inputs=["query"],outputs=["result"])
        self.assertEqual(len(app._events),1)
        self.assertEqual(app._events[0][0]["outputs"],["result"])
        with self.assertRaises(ValueError):
            app.on("run","click",lambda x:x,inputs=["query"],outputs=["result"])
        with self.assertRaises(ValueError):
            app.on("run","change",lambda x:x,inputs=["query"],outputs=["result"])
        with self.assertRaises(ValueError):
            app.on("run","click",lambda x:x,inputs=["missing"],outputs=["result"])

    def test_invalid_references(self):
        q=gr.Textbox(key="query")
        r=gr.Markdown(key="result")
        bad=[
            gr.Row(gr.Ref("query"),gr.Ref("query"),gr.Ref("result")),
            gr.Row(gr.Ref("query"),gr.Ref("result"),gr.Button("query","Duplicado")),
            gr.Tabs(gr.Ref("query")),
            gr.Row(gr.Tab("Fuera",gr.Ref("query")),gr.Ref("result")),
            gr.Row(gr.Ref("query"),gr.Ref("result"),gr.Button("run",""),gr.Button("run","Duplicado")),
        ]
        for layout in bad:
            with self.subTest(layout=layout),self.assertRaises((ValueError,TypeError)):
                gr.Blocks(fn=lambda v:v,inputs=[q],outputs=[r],layout=layout)

    def test_editable_table_and_code(self):
        code=gr.Code(value="print(1)", key="source", language="python", lines=12)
        sheet=gr.Dataframe(key="sheet", headers=["Nombre","Edad"],datatype=["str","number"],
                           row_count=3, column_count=2, fixed_columns=True, show_search=False)
        self.assertEqual(code.language,"python")
        self.assertEqual(code.lines,12)
        self.assertFalse(sheet.show_search)
        self.assertTrue(sheet.fixed_columns)
        self.assertEqual(sheet.headers,["Nombre","Edad"])

    def test_legacy_interface_untouched(self):
        a=gr.Interface(fn=lambda x:x,inputs=gr.Textbox(),outputs=gr.Textbox())
        self.assertIsNone(a.layout)
        with self.assertRaises(ValueError):
            gr.Blocks(fn=lambda x:x,inputs=[gr.Textbox()],outputs=[gr.Textbox()],
                      layout=gr.Row(gr.Ref("query")))
