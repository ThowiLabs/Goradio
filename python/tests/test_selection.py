import unittest
import goradio as gr

class SelectionTests(unittest.TestCase):
    def test_native_helpers(self):
        radio=gr.Radio(["Rojo","Azul"], label="Color",value="Azul")
        group=gr.CheckboxGroup(["Go","Python","Rust"],value=["Go"])
        self.assertEqual(radio.type,"radio")
        self.assertEqual(group.type,"checkboxgroup")
        self.assertEqual(group.choices,["Go","Python","Rust"])
        self.assertEqual(group.value,["Go"])
    def test_exposes_valid_config(self):
        demo=gr.Interface(fn=lambda color,langs:str([color,langs]),
                          inputs=[gr.Radio(["A","B"]),gr.CheckboxGroup(["Go","Python"])],
                          outputs=gr.Textbox())
        self.assertEqual([x.type for x in demo.inputs],["radio","checkboxgroup"])

if __name__=="__main__":unittest.main()
