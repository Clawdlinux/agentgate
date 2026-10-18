"""Local decision backends. No downloader or remote code execution."""

import hashlib
import json
import re
import time
from pathlib import Path

QUESTIONS = {"task_consistent", "personal_data", "risk"}
SOURCE_URL = "https://pypi.org/project/vllm-sr/0.4.0/"
REVISION = re.compile(r"^[a-f0-9]{40}$")


class HeuristicBackend:
    """Deterministic test fixture, not a trained decision model."""

    def __init__(self):
        self.revision = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        self.weights_sha256 = hashlib.sha256(b"agentgate-heuristic-v1:no-weights").hexdigest()

    def decide(self, state, questions, tier="A"):
        start = time.monotonic_ns()
        action = str(state.get("action", "")).lower()
        params = state.get("params") or {}
        if not isinstance(params, dict):
            raise ValueError("params must be an object")
        payload = json.dumps(params, sort_keys=True).lower()
        risk, consistent, personal = 0.1, 0.5, 0.05
        if any(word in action for word in ("delete", "drop", "cancel", "revoke")):
            risk = 0.75
        if any(word in action for word in ("payment", "charge", "transfer", "refund")):
            risk = 0.95 if numeric(params.get("amount")) > 10000 else 0.7
        if "export" in action:
            risk = 0.95 if numeric(params.get("count")) > 100 else 0.7
        if params.get("bulk") is True:
            risk = 0.95
        if params.get("public") is True or params.get("role") == "admin":
            risk = max(risk, 0.75)
        recipient = str(params.get("to", ""))
        if "@" in recipient and not recipient.endswith("@corp.example"):
            risk = max(risk, 0.75)
        if any(key in payload for key in ('"ssn"', '"customer_email"', '"passport"')):
            personal = 0.75
        if any(key in payload for key in ('"password"', '"access_token"', '"private_key"')):
            personal, risk = 0.99, 0.99
        # The gateway has no separate task signal. Payload fields cannot supply that authority.
        confidence = 0.7
        return {
            "tier": tier,
            "model_id": "agentgate/heuristic-v1",
            "revision": self.revision,
            "weights_sha256": self.weights_sha256,
            "probabilities": {
                "task_consistent": consistent,
                "personal_data": personal,
                "risk": risk,
            },
            "confidence": confidence,
            "latency_us": max(0, (time.monotonic_ns() - start) // 1000),
        }


def numeric(value):
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return value
    return 0


class VLLMSRBackend:
    """Blocked until the upstream decision-model inference API is verified."""

    def __init__(self, model_dir, revision, **_kwargs):
        if not REVISION.fullmatch(revision or ""):
            raise ValueError("an exact 40-character model revision is required")
        if not model_dir or not Path(model_dir).is_dir():
            raise ValueError("a read-only local model directory is required")
        raise NotImplementedError(
            "vllm-sr 0.4.0 publishes routing CLI code, not a verified "
            f"Decision 2.0 inference contract. See {SOURCE_URL} and RUNTIME.md."
        )


class ONNXBackend(VLLMSRBackend):
    """No custom loader. Await a verified upstream ONNX decision runtime."""


class Cascade:
    def __init__(self, tier_a, tier_b=None, tier_c=None, confidence_threshold=0.8):
        if not 0 < confidence_threshold <= 1:
            raise ValueError("confidence_threshold must be in (0,1]")
        self.backends = [backend for backend in (tier_a, tier_b, tier_c) if backend is not None]
        if not self.backends or (tier_c is not None and tier_b is None):
            raise ValueError("cascade tiers must be contiguous")
        self.confidence_threshold = confidence_threshold

    def decide(self, state, questions):
        deadline = time.monotonic() + 0.150
        attempts = []
        for index, backend in enumerate(self.backends):
            if time.monotonic() >= deadline:
                raise CascadeUnavailable(attempts)
            try:
                result = backend.decide(state, questions, tier=chr(ord("A") + index))
            except Exception:
                raise CascadeUnavailable(attempts) from None
            attempts.append(result)
            if time.monotonic() >= deadline:
                raise CascadeUnavailable(attempts)
            if result["confidence"] >= self.confidence_threshold:
                break
        return {"attempts": attempts, "cascade_threshold": self.confidence_threshold}


class CascadeUnavailable(Exception):
    def __init__(self, attempts):
        self.attempts = attempts
        super().__init__("advisor deadline exceeded")
