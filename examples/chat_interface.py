"""Use your own AI model in an original Gradio chatbot, without an HTML UI."""
import goradio as gr
import time

def model(message, history):
    # Replace this function with an actual local or hosted model.
    for state in ("Procesando", "Generando respuesta", f"Eco: {message} (historial: {len(history)})"):
        yield state
        time.sleep(.15)

chat=gr.ChatInterface(
    fn=model,
    title="ChatInterface · Goradio",
    description="Demostración de conexión de modelo: respuestas de ejemplo.",
)

if __name__ == "__main__":
    chat.launch(server_port=7990)
