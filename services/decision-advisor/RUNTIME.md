# Decision runtime inspection

Inspected on 2026-10-18. No package code was executed or installed.

## Published vllm-sr 0.4.0

Sources:

- [PyPI metadata](https://pypi.org/pypi/vllm-sr/0.4.0/json).
- [Versioned package](https://pypi.org/project/vllm-sr/0.4.0/).
- [Upstream repository](https://github.com/vllm-project/vllm-semantic-router).

| Artifact | SHA-256 |
| --- | --- |
| `vllm_sr-0.4.0-py3-none-any.whl` | `e027138d61918f9bc357b5918bf366248ce7b4d3aa428c20f9f04030c3a3c4ea` |
| `vllm_sr-0.4.0.tar.gz` | `19e472802aee2583bc6a49ef7239bfdd20f34cb72e0b4d9a21f0a003e622c6c0` |

We read the wheel in memory and checked its SHA-256 against PyPI metadata.
The package contains routing CLI and configuration code under `cli/`.
`cli/commands/route.py` probes a router's management or OpenAI endpoints.
`cli/config_migration_model_runtime.py` references `http_chat`, `http_classify`, and `label_decision.v1`.
Those references do not specify Decision 2.0 model loading or state/questions inference.
No decision-model Python inference entry point was verified.

Therefore `VLLMSRBackend` raises `NotImplementedError` after checking local paths and revision shape.
`ONNXBackend` shares that explicit blocker.
Neither downloads weights, imports model repositories, or invokes guessed APIs.
No runtime dependency is installed by the Dockerfile.

## Pinned model inspection

| Candidate | Inspected revision | Listed weight bytes |
| --- | --- | --- |
| `vllm-sr/Decision-2.0-Kai-0.6B` | `cc7f30cda9e7c7b28c5f71ef33f01e09385f1c4b` | 1,503,429,208 backbone + 4,213,584 head |
| `convaiinnovations/laya` | `7b928d828b7b0e022f929d9bd2e44165aa270148` | 842,609,210 root model |

Source: Hugging Face model metadata and recursive file trees at those revisions.
Neither inspected tree listed an `.onnx` file.
Other ONNX publications may exist. We did not verify their runtime contract.
`onnxruntime` is not installed on this host.
Kai's listed weights total 1,507,642,792 bytes, above the requested 1.5 GB cap.
Laya fits that cap, but its listed root artifact is not ONNX.
Downloading it would not establish a supported decision runtime.
We downloaded no weights and ran no model benchmark.

## Unblocking model backends

Verify an upstream runtime release with a documented Decision 2.0 inference API.
Scan its pinned package sources before installing or trusting it.
Pin all dependencies with hashes.
Keep model loading inside the upstream runtime.
Pin model revisions and verify every mounted weight against a SHA-256 manifest.
Never execute repository-supplied Python code.
Mirror runtime images and weights before entering the air-gapped environment.
Use read-only mounts and deny all advisor network egress.
Add a durable tier journal for calls whose responses exceed the gateway deadline.
Measure Kai and Laya before selecting either.

Lux and Vega revisions remain explicit deployment placeholders.
Tier B requires a GPU with at least 24 GB.
Tier C remains optional and disabled. Its GPU needs at least 64 GB.
Upstream reports AMD MI300X/MI325X validation.
NVIDIA runs are not yet upstream-validated.
Apple silicon evaluation is CPU-only.
