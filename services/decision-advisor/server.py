"""Isolated decision service. Inputs are untrusted and outputs are advisory."""

import argparse
import json
import math
import os
import socket
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from backends import (
    Cascade,
    CascadeUnavailable,
    HeuristicBackend,
    ONNXBackend,
    QUESTIONS,
    VLLMSRBackend,
)

MAX_INPUT_BYTES = 1 << 20


def reject_constant(_value):
    raise ValueError("non-finite JSON number")


def finite_float(value):
    result = float(value)
    if not math.isfinite(result):
        raise ValueError("non-finite JSON number")
    return result


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON key")
        result[key] = value
    return result


def validate_request(body):
    value = json.loads(
        body, parse_constant=reject_constant, parse_float=finite_float, object_pairs_hook=unique_object
    )
    if not isinstance(value, dict) or set(value) != {"state", "questions"}:
        raise ValueError("state and questions are required")
    if not isinstance(value["state"], (dict, str)):
        raise ValueError("state must be an object or text")
    questions = value["questions"]
    if not isinstance(questions, dict) or set(questions) != QUESTIONS:
        raise ValueError("the fixed question set is required")
    for key, question in questions.items():
        kind = "score" if key == "risk" else "yes_no"
        if not isinstance(question, dict) or set(question) != {"text", "kind"}:
            raise ValueError("invalid question")
        if question["kind"] != kind or not isinstance(question["text"], str) or len(question["text"]) > 256:
            raise ValueError("invalid question")
    state = value["state"]
    if isinstance(state, str):
        state = {"text": state}
    return state, questions


class AdvisorServer(ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 64

    def __init__(self, address, cascade, max_input_bytes=MAX_INPUT_BYTES, max_workers=8):
        self.cascade = cascade
        self.max_input_bytes = max_input_bytes
        self.slots = threading.BoundedSemaphore(max_workers)
        self.metrics_lock = threading.Lock()
        self.metrics = {"requests": 0, "errors": 0, "tier_calls": 0}
        super().__init__(address, Handler)

    def process_request(self, request, client_address):
        request.settimeout(0.150)
        if not self.slots.acquire(blocking=False):
            try:
                request.sendall(b"HTTP/1.0 503 Service Unavailable\r\nContent-Length: 0\r\n\r\n")
            finally:
                self.shutdown_request(request)
            return
        try:
            super().process_request(request, client_address)
        except Exception:
            self.slots.release()
            raise

    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.slots.release()

    def count(self, **values):
        with self.metrics_lock:
            for key, value in values.items():
                self.metrics[key] += value


class Handler(BaseHTTPRequestHandler):
    server_version = "decision-advisor/1"

    def log_message(self, _format, *_args):
        pass

    def send_json(self, status, value):
        body = json.dumps(value, allow_nan=False, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            self.send_json(200, {"status": "ok"})
        elif self.path == "/metrics":
            with self.server.metrics_lock:
                values = dict(self.server.metrics)
            body = "".join(f"advisor_{key}_total {value}\n" for key, value in values.items()).encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; version=0.0.4")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        else:
            self.send_json(404, {"error": "not_found"})

    def do_POST(self):
        if self.path != "/v1/decide":
            self.send_json(404, {"error": "not_found"})
            return
        self.server.count(requests=1)
        lengths = self.headers.get_all("Content-Length", [])
        if self.headers.get("Transfer-Encoding") or len(lengths) > 1:
            self.send_json(400, {"error": "invalid_framing"})
            return
        if not lengths:
            self.send_json(411, {"error": "length_required"})
            return
        if self.headers.get_content_type() != "application/json" or self.headers.get("Content-Encoding"):
            self.send_json(415, {"error": "json_required"})
            return
        try:
            if not lengths[0].isdigit():
                raise ValueError("invalid content length")
            size = int(lengths[0])
            if size > self.server.max_input_bytes:
                self.send_json(413, {"error": "input_too_large"})
                return
            body = self.rfile.read(size)
            if len(body) != size:
                raise ValueError("incomplete body")
            state, questions = validate_request(body)
            result = self.server.cascade.decide(state, questions)
            self.server.count(tier_calls=len(result["attempts"]))
            self.send_json(200, result)
        except (ValueError, RecursionError, UnicodeError, socket.timeout):
            self.server.count(errors=1)
            self.send_json(400, {"error": "invalid_input"})
        except CascadeUnavailable as error:
            self.server.count(errors=1, tier_calls=len(error.attempts))
            self.send_json(503, {"attempts": error.attempts, "cascade_threshold": self.server.cascade.confidence_threshold})
        except Exception:
            self.server.count(errors=1)
            self.send_json(503, {"attempts": [], "cascade_threshold": self.server.cascade.confidence_threshold})


def make_backend(kind, tier):
    if kind == "heuristic":
        return HeuristicBackend()
    constructors = {"onnx": ONNXBackend, "vllm-sr": VLLMSRBackend}
    if kind not in constructors:
        raise ValueError("unknown backend")
    return constructors[kind](
        model_dir=os.environ.get(f"ADVISOR_TIER_{tier}_MODEL_DIR"),
        revision=os.environ.get(f"ADVISOR_TIER_{tier}_REVISION"),
    )


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8081)
    args = parser.parse_args()
    threshold = float(os.environ.get("ADVISOR_CONFIDENCE_THRESHOLD", "0.8"))
    if not math.isfinite(threshold):
        raise ValueError("non-finite threshold")
    tier_b = os.environ.get("ADVISOR_TIER_B_BACKEND", "")
    tier_c = os.environ.get("ADVISOR_TIER_C_BACKEND", "")
    cascade = Cascade(
        make_backend(os.environ.get("ADVISOR_BACKEND", "heuristic"), "A"),
        make_backend(tier_b, "B") if tier_b else None,
        make_backend(tier_c, "C") if tier_c else None,
        confidence_threshold=threshold,
    )
    server = AdvisorServer((args.host, args.port), cascade)
    try:
        server.serve_forever()
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
