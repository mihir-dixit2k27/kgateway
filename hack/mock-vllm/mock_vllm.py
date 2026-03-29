from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import os
import time


HOST = os.getenv("MOCK_HOST", "0.0.0.0")
PORT = int(os.getenv("MOCK_PORT", "8000"))
MODEL = os.getenv("MOCK_MODEL", "llama3")
RESPONSE_DELAY_MS = int(os.getenv("RESPONSE_DELAY_MS", "8"))


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def _json(self, status_code, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status_code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        if self.path != "/v1/chat/completions":
            self._json(404, {"error": "not found"})
            return

        content_length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(content_length)
        try:
            req = json.loads(raw.decode("utf-8")) if raw else {}
        except json.JSONDecodeError:
            self._json(400, {"error": "invalid json"})
            return

        time.sleep(RESPONSE_DELAY_MS / 1000.0)
        model = req.get("model", MODEL)
        payload = {
            "id": "chatcmpl-mock-1",
            "object": "chat.completion",
            "created": int(time.time()),
            "model": model,
            "choices": [
                {
                    "index": 0,
                    "message": {
                        "role": "assistant",
                        "content": "hello from mock-vllm",
                    },
                    "finish_reason": "stop",
                }
            ],
            "usage": {
                "prompt_tokens": 8,
                "completion_tokens": 5,
                "total_tokens": 13,
            },
        }
        self._json(200, payload)


if __name__ == "__main__":
    server = HTTPServer((HOST, PORT), Handler)
    print(f"mock-vllm listening on {HOST}:{PORT}")
    server.serve_forever()
