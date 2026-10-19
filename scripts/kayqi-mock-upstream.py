"""仅用于本机验收的 OpenAI 兼容模拟上游，不连接真实模型，也不验证真实模型参数。"""

import argparse
import base64
import json
import struct
import time
import zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def test_image():
    """生成确定性的测试色块，便于核对生成图片是否真正落盘。"""
    width, height = 256, 256
    pixels = b"".join(
        b"\x00" + bytes((36, 96 + y // 3, 164)) * width for y in range(height)
    )

    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))

    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(pixels)) + chunk(b"IEND", b"")


class Handler(BaseHTTPRequestHandler):
    # 禁止记录聊天正文、上传图片和凭据。
    def log_message(self, *_args):
        pass

    def do_GET(self):
        self.reply({"object": "list", "data": [{"id": "mock-chat"}, {"id": "mock-image"}]})

    def reply(self, value):
        body = json.dumps(value, ensure_ascii=False).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        if "/images/" in self.path:
            # 保留一点可观察的执行时间，便于验收停止与刷新恢复。
            time.sleep(self.server.delay)
            self.reply({"created": int(time.time()), "data": [{"b64_json": base64.b64encode(test_image()).decode()}], "usage": {"input_tokens": 12, "output_tokens": 24}})
            return
        request = json.loads(body or b"{}")
        text = "本地模拟回复：已收到消息。图片理解输入会保留在同一会话中。"
        if not request.get("stream"):
            self.reply({"id": "mock-chat-response", "model": "mock-chat", "choices": [{"message": {"role": "assistant", "content": text}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 12, "completion_tokens": 24, "total_tokens": 36}})
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for payload in [
            {"id": "mock-chat-response", "choices": [{"index": 0, "delta": {"role": "assistant", "content": text}, "finish_reason": None}]},
            {"id": "mock-chat-response", "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 12, "completion_tokens": 24, "total_tokens": 36}},
        ]:
            self.wfile.write(("data: " + json.dumps(payload, ensure_ascii=False) + "\n\n").encode())
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=18081)
    parser.add_argument("--delay", type=float, default=1)
    args = parser.parse_args()
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    server.delay = max(0, args.delay)
    print(f"Mock upstream: http://127.0.0.1:{args.port}/v1", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
