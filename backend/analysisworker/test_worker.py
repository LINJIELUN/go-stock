import hashlib
import json
import os
import subprocess
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

WORKER = Path(__file__).with_name("worker.py")


class ProviderHandler(BaseHTTPRequestHandler):
    request_body = None
    authorization = None
    mode = "success"
    redirected = False

    def do_POST(self):
        length = int(self.headers["Content-Length"])
        type(self).request_body = json.loads(self.rfile.read(length))
        type(self).authorization = self.headers.get("Authorization")
        if self.path == "/redirect-target":
            type(self).redirected = True
        if type(self).mode == "redirect":
            self.send_response(307)
            self.send_header("Location", f"http://127.0.0.1:{self.server.server_port}/redirect-target")
            self.end_headers()
            return
        analysis = {
            "schemaVersion": "trading-analysis-output-v0.1",
            "probabilityNotice": "模型估计、非实际结果",
            "riseProbability": 55,
        }
        response = {"choices": [{"message": {"content": json.dumps(analysis)}}]}
        if type(self).mode != "missing-usage":
            response["usage"] = {"prompt_tokens": 123, "completion_tokens": 45}
        encoded = json.dumps(response).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def log_message(self, *_args):
        pass


class WorkerTests(unittest.TestCase):
    def setUp(self):
        ProviderHandler.mode = "success"
        ProviderHandler.redirected = False
        ProviderHandler.request_body = None
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), ProviderHandler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def request(self):
        bundle = {"schemaVersion": "analysis-input-bundle-v0.3", "stockCode": "600000"}
        canonical = json.dumps(bundle, ensure_ascii=False, separators=(",", ":")).encode()
        return {"protocolVersion": "isolated-analysis-engine-v0.1",
                "bundleHash": hashlib.sha256(canonical).hexdigest(), "bundle": bundle,
                "model": "test-model", "promptVersion": "prompt-v1"}

    def run_worker(self, request, **overrides):
        env = {"MODEL_API_ENDPOINT": f"http://127.0.0.1:{self.server.server_port}/v1/chat/completions",
               "MODEL_API_KEY": "test-secret", "MODEL_ID": "test-model", "ALLOW_HTTP_LOOPBACK": "1",
               "MODEL_INPUT_USD_PER_MILLION": "1.5", "MODEL_OUTPUT_USD_PER_MILLION": "2"}
        env.update(overrides)
        return subprocess.run([sys.executable, str(WORKER)], input=json.dumps(request).encode(),
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, check=False)

    def test_forwards_frozen_bundle_and_returns_bound_protocol(self):
        result = self.run_worker(self.request())
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        response = json.loads(result.stdout)
        self.assertEqual(response["bundleHash"], self.request()["bundleHash"])
        self.assertEqual(response["inputTokens"], 123)
        self.assertTrue(response["actualCostKnown"])
        self.assertAlmostEqual(response["actualCostUsd"], 0.0002745)
        self.assertEqual(ProviderHandler.authorization, "Bearer test-secret")
        user_message = ProviderHandler.request_body["messages"][1]["content"]
        self.assertEqual(json.loads(user_message)["stockCode"], "600000")

    def test_rejects_model_mismatch_without_provider_request(self):
        ProviderHandler.request_body = None
        request = self.request()
        request["model"] = "unapproved-model"
        result = self.run_worker(request)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, b"")
        self.assertIsNone(ProviderHandler.request_body)
        self.assertNotIn(b"test-secret", result.stderr)

    def test_rejects_non_https_remote_endpoint(self):
        result = self.run_worker(self.request(), MODEL_API_ENDPOINT="http://example.com/v1/chat/completions")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b"must use HTTPS", result.stderr)

    def test_rejects_invalid_pricing_before_provider_request(self):
        ProviderHandler.request_body = None
        result = self.run_worker(self.request(), MODEL_INPUT_USD_PER_MILLION="NaN")
        self.assertNotEqual(result.returncode, 0)
        self.assertIsNone(ProviderHandler.request_body)
        self.assertIn(b"MODEL_INPUT_USD_PER_MILLION", result.stderr)

    def test_rejects_missing_usage_instead_of_claiming_zero_cost(self):
        ProviderHandler.mode = "missing-usage"
        result = self.run_worker(self.request())
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, b"")
        self.assertIn(b"response contract", result.stderr)

    def test_rejects_redirect_without_forwarding_authorization(self):
        ProviderHandler.mode = "redirect"
        result = self.run_worker(self.request())
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(ProviderHandler.redirected)
        self.assertNotIn(b"test-secret", result.stderr)


if __name__ == "__main__":
    unittest.main()
