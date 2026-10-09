# Third-party FRP protocol attribution

The files `msg.go` and `ctl.go` in this directory are adapted from the
Gradio-compatible FRP fork published at https://github.com/huggingface/frp
(revision `43a1bfcf97e3f6b52e9cbe70edda55bc1743d9ab`), based on
https://github.com/fatedier/frp. Original copyright and Apache-2.0
headers have been retained.

Changes: package renamed to `frpmsg`; imported as an internal
Goradio package to avoid requiring consumers to replace the module path
for the fork. See the accompanying LICENSE for the full license text.
