#!/usr/bin/env python3
"""Run destructive bulk lifecycle tests in a new synthetic loopback Brain instance.

Usage: python3 scripts/test-bulk-lifecycle.py [--binary /path/to/brain]
Requires Go and the web npm dependencies. No production config, credentials,
runner, embeddings, or data are used. The temporary fixture/logs are retained.
"""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.request
import uuid

repo = Path(__file__).resolve().parent.parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--binary", type=Path)
parser.add_argument("--hold-seconds", type=int, default=0, help="Keep the disposable server available for browser verification after tests")
args = parser.parse_args()
root = Path(tempfile.mkdtemp(prefix="brain-bulk-lifecycle-"))
data = root / "data"
config = root / "config" / "brain"
config.mkdir(parents=True)
with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
base = f"http://127.0.0.1:{port}"
(config / "config.yaml").write_text(f"""server:
  host: 127.0.0.1
  port: {port}
  brain_dir: {data}
  enable_auth: false
  feature_checkout:
    enabled: false
  feature_delivery:
    enabled: false
  embedding:
    enabled: false
""")

def seed(project, group, count, status="completed", remote=""):
    folder = data / "projects" / project / "task"
    folder.mkdir(parents=True, exist_ok=True)
    for i in range(count):
        # Mix absent and stale project metadata with current files.
        project_field = "projectId: stale-project\n" if i % 3 == 0 else ""
        content = f"---\ntitle: Synthetic {group} {i}\ntype: task\nstatus: {status}\nfeature_id: {group}\n{project_field}"
        if remote:
            content += f"git_remote: {remote}\n"
        (folder / f"{group}-{i:04d}.md").write_text(content + "---\n\nSynthetic content. Disposable test data.\n")

seed("restart_test", "crash", 1000)
seed("bulk_test", "legacy", 278, remote="ssh://git@github.com/example/synthetic.git")
seed("bulk_test", "normal", 546)
seed("bulk_test", "archived", 142, "archived")
seed("bulk_test", "cancelled", 130, "cancelled")
seed("bulk_test", "status-moves", 205)
seed("bulk_test", "move", 125)
seed("bulkXtest", "archived", 42, "archived")
seed("bulk_test-other", "archived", 42, "archived")
for project, body in [("bulk_test", "Source collision"), ("destination", "Existing destination")]:
    folder = data / "projects" / project / "task"
    folder.mkdir(parents=True, exist_ok=True)
    (folder / "collision.md").write_text(f"---\ntitle: Collision\ntype: task\nstatus: completed\n---\n{body}\n")

binary = args.binary.resolve() if args.binary else root / "brain"
if not args.binary:
    subprocess.run(["go", "build", "-o", str(binary), "./cmd/brain"], cwd=repo, check=True)
env = {k: v for k, v in os.environ.items() if not k.startswith("BRAIN_")}
env.update({"XDG_CONFIG_HOME": str(root / "config"), "XDG_STATE_HOME": str(root / "state"),
            "BRAIN_DIR": str(data), "HOST": "127.0.0.1", "PORT": str(port), "ENABLE_AUTH": "false",
            "BRAIN_FEATURE_CHECKOUT_ENABLED": "false", "BRAIN_FEATURE_DELIVERY_ENABLED": "false",
            "RATE_LIMIT_PER_MINUTE": "0", "BRAIN_SYNTHETIC_TEST": "1"})
log_path = root / "server.log"
print(f"Isolated fixture: {root}\nServer: {base}", flush=True)
with log_path.open("w") as log:
    server = subprocess.Popen([str(binary), "api", "--host", "127.0.0.1", "--port", str(port)], env=env, stdout=log, stderr=log)
    try:
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            if server.poll() is not None:
                raise RuntimeError(log_path.read_text())
            if 'msg="indexing complete"' in log_path.read_text():
                break
            time.sleep(0.1)
        else:
            raise RuntimeError("Initial synthetic indexing did not complete")
        with urllib.request.urlopen(base + "/api/v1/health", timeout=5) as response:
            assert json.load(response)["status"] == "healthy"
        result = subprocess.run(["node", "--import", "tsx", "scripts/bulk-lifecycle.ts", base], cwd=repo / "web", env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=600)
        (root / "results.log").write_text(result.stdout)
        print(result.stdout, end="", flush=True)
        result.check_returncode()
        assert len(list((data / "projects" / "destination" / "task").glob("*.md"))) == 126
        assert len(list((data / "projects" / "bulk_test" / "task").glob("*.md"))) == 1
        print("PASS: final file counts agree with API results", flush=True)
        def request(path, body=None):
            raw = None if body is None else json.dumps(body).encode()
            req = urllib.request.Request(base + "/api/v1/" + path, data=raw,
                                         headers={"Content-Type": "application/json"})
            with urllib.request.urlopen(req, timeout=20) as response:
                return json.load(response)

        body = {"request_id": str(uuid.uuid4()), "operation": "delete",
                "filters": [{"project": "restart_test", "type": "task"}]}
        job = request("bulk-jobs", body)
        for _ in range(200):
            progress = request("bulk-jobs/" + job["id"])
            if progress["succeeded"] > 0 and progress["pending"] > 0:
                break
            time.sleep(.01)
        else:
            raise AssertionError("Did not observe a partially executed crash-test job")
        server.kill()
        server.wait(timeout=10)
        print(f"Killed server after {progress['succeeded']} confirmed deletions; {progress['pending']} pending", flush=True)
        server = subprocess.Popen([str(binary), "api", "--host", "127.0.0.1", "--port", str(port)], env=env, stdout=log, stderr=log)
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            try:
                progress = request("bulk-jobs/" + job["id"])
                if progress["state"] in ("completed", "needs_attention"):
                    break
            except OSError:
                pass
            time.sleep(.1)
        else:
            raise AssertionError("Restart did not finish pending work")
        assert progress["pending"] == 0 and progress["running"] == 0 and progress["failed"] == 0, progress
        assert progress["succeeded"] + progress["uncertain"] == 1000, progress
        assert progress["uncertain"] <= 1, progress
        assert request("bulk-jobs", body)["id"] == job["id"], "idempotency survives restart"
        for offset in range(0, 1000, 200):
            items = request(f"bulk-jobs/{job['id']}/items?offset={offset}&limit=200")
            assert len(items) == 200
            assert all(i["attempts"] == 1 for i in items), "an item was replayed"
        remaining = len(list((data / "projects" / "restart_test" / "task").glob("*.md")))
        assert remaining <= progress["uncertain"], (remaining, progress)
        print("PASS: kill/restart recovery; each of 1000 targets attempted exactly once; " + json.dumps(progress), flush=True)
        if args.hold_seconds:
            print(f"Browser verification server available for {args.hold_seconds}s at {base}", flush=True)
            time.sleep(args.hold_seconds)

    finally:
        server.terminate()
        try:
            server.wait(timeout=10)
        except subprocess.TimeoutExpired:
            server.kill()
            server.wait()
