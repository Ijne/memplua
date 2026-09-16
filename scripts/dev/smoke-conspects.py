"""Run a built Windows/core executable against a deterministic local LLM fixture.

No model, microphone, user database or user vault is accessed. For manual/browser
inspection, --hold-file keeps the isolated fixture alive until that file is removed.
"""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import threading
import tempfile
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--work-dir", required=True)
    parser.add_argument("--hold-file")
    args = parser.parse_args()
    root = Path(args.work_dir).resolve()
    root.mkdir(parents=True, exist_ok=True)
    root = Path(tempfile.mkdtemp(prefix="conspect-", dir=root))
    token = "isolated-conspect-smoke-token"
    calls = []

    class Fixture(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b'{"status":"ok"}')

        def do_POST(self):
            request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            focus = json.JSONDecoder().raw_decode(request["prompt"].split("\nFOCUS:\n", 1)[1])[0]
            calls.append(focus)
            evidence = {"evidence_chunk_ids": [focus[0]["chunk_id"]], "anchor_chunk_id": focus[0]["chunk_id"]}
            groups = []
            for topic, term, definition in [
                ("Context handling", "Context Tail", "The bounded tail of a preceding batch provides context for new facts."),
                ("Atomic review", "Apply Conspect", "All accepted knowledge changes commit in one transaction."),
                ("Durable batching", "AnalysisBatch", "Every focus chunk belongs to exactly one durable analysis batch."),
            ]:
                terms = [{"ref": "term-1", "name": term, "description": definition, "tags": ["technology"], **evidence}]
                if len(calls) > 1:
                    terms.append({"ref": "term-2", "name": term, "description": definition + " Preserve its evidence and source identity.", "tags": ["batch-review"], **evidence})
                groups.append({"topic": topic, "terms": terms, "thoughts": [{"thesis": topic + " preserves knowledge provenance", "body": "Review source evidence before accepting the extracted claim.", "related_term_refs": ["term-1"], "tags": ["idea"], **evidence}]})
            payload = json.dumps({"content": json.dumps({"conspects": groups})}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

    fixture = ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    threading.Thread(target=fixture.serve_forever, daemon=True).start()
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    config = root / "smoke.toml"
    config.write_text(f'''data_dir = "{root.as_posix()}"
[storage]
database = "knowledge.db"
[api]
listen = "127.0.0.1:{port}"
token = "{token}"
[models]
managed = false
llm_url = "http://127.0.0.1:{fixture.server_port}"
[pipeline]
poll_interval = "50ms"
batch_sweep_interval = "100ms"
[logging]
console = false
''', encoding="utf-8")
    base = f"http://127.0.0.1:{port}"

    def api(path, method="GET", body=None):
        data = json.dumps(body).encode() if body is not None else None
        request = urllib.request.Request(base + path, data=data, method=method, headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
        with urllib.request.urlopen(request, timeout=10) as response:
            payload = response.read()
            return json.loads(payload) if payload else None

    def wait_for(check, timeout=30):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                value = check()
                if value:
                    return value
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(0.1)
        raise AssertionError("smoke check timed out")

    with (root / "process.log").open("w", encoding="utf-8") as log:
        process = subprocess.Popen([str(Path(args.binary).resolve()), "serve", "--config", str(config)], stdout=log, stderr=log, creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0)
        try:
            wait_for(lambda: api("/healthz"))
            api("/api/v1/ingest/text", "POST", {"text": "Context tails, atomic review and durable batches preserve source evidence."})
            conspects = wait_for(lambda: (items if len(items := api("/api/v1/review/conspects")) == 3 and all(c["status"] == "review_pending" for c in items) else None))
            assert len(calls) == 1, "multiple extraction calls for a single batch"
            assert api("/api/v1/graph")["revision"] == 0
            for c in conspects:
                assert len(c["items"]) == 2, "technical links appeared as review items"
                for item in c["items"]:
                    api("/api/v1/review/items/" + item["id"], "PUT", {"resolution": "create"})
                revision = api("/api/v1/graph")["revision"]
                api("/api/v1/review/conspects/" + c["id"] + "/apply", "POST")
                api("/api/v1/review/conspects/" + c["id"] + "/apply", "POST")
                assert api("/api/v1/graph")["revision"] == revision + 1
            graph = api("/api/v1/graph")
            assert len(graph["terms"]) == len(graph["thoughts"]) == len(graph["links"]) == 3
            api("/api/v1/ingest/text", "POST", {"text": "Additional evidence for the same topics."})
            conspects = wait_for(lambda: (items if len(items := api("/api/v1/review/conspects")) == 3 and all(c["status"] == "review_pending" for c in items) else None))
            assert len(calls) == 2
            assert all(any(len(i["incoming_variants"]) == 2 and i["canonical_matches"] for i in c["items"]) for c in conspects)
            events = api("/api/v1/notifications?limit=500")
            assert {"analysis_batch.closed", "analysis_batch.extracted", "conspect.review_ready", "conspect.applied"} <= {e["type"] for e in events}
            print(json.dumps({"result": "passed", "url": base + "/ui/", "token": token, "llm_calls": len(calls), "graph_revision": graph["revision"], "review_conspects": len(conspects)}), flush=True)
            if args.hold_file:
                hold = Path(args.hold_file).resolve()
                hold.write_text("remove to stop isolated smoke application", encoding="utf-8")
                deadline = time.monotonic() + 600
                while hold.exists() and time.monotonic() < deadline:
                    time.sleep(0.5)
            api("/api/v1/application/shutdown", "POST")
            process.wait(timeout=10)
            assert process.returncode == 0, f"application exited with {process.returncode}"
        finally:
            if process.poll() is None:
                process.terminate()
                process.wait(timeout=10)
            fixture.shutdown()


if __name__ == "__main__":
    main()
