"""Real Redis -> managed task -> SQLite regression, using disposable instances only.

Build bin/redis-shake-web-dev-next first, or set TEST_BINARY to another build.
Requires Docker and the local redis:7.2 image. No existing Redis is contacted.
"""
import http.cookiejar
import json
import os
from pathlib import Path
import socket
import sqlite3
import subprocess
import time
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]
BINARY = Path(os.environ.get("TEST_BINARY", ROOT / "bin/redis-shake-web-dev-next")).resolve()


def wait_for(check, timeout=20):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        result = check()
        if result:
            return result
        time.sleep(0.05)
    raise AssertionError("Timed out waiting for test condition")


def redis(port, *args):
    with socket.create_connection(("127.0.0.1", port), timeout=5) as conn:
        data = [str(arg).encode() for arg in args]
        conn.sendall(b"*%d\r\n" % len(data) + b"".join(b"$%d\r\n" % len(arg) + arg + b"\r\n" for arg in data))
        stream = conn.makefile("rb")
        line = stream.readline()
        if line.startswith(b"$"):
            size = int(line[1:])
            return None if size < 0 else stream.read(size).decode()
        if line.startswith(b"-"):
            raise AssertionError(line.decode())
        return line[1:-2].decode()


def main():
    token = uuid.uuid4().hex[:8]
    runtime = ROOT / ".redis-shake-web/dev" / ("offset-e2e-" + token)
    runtime.mkdir(parents=True)
    # Unix sockets have a short path limit on macOS; keep this under the runtime root.
    containers, processes, logs = [], [], []
    csrf = ""
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        web_port = reservation.getsockname()[1]
    base = f"http://127.0.0.1:{web_port}"
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def api(path, body=None):
        headers = {"Content-Type": "application/json", "X-CSRF-Token": csrf}
        request = urllib.request.Request(base + "/api" + path, data=None if body is None else json.dumps(body).encode(), headers=headers)
        with opener.open(request, timeout=20) as response:
            return json.load(response)

    def start(mode):
        log = open(runtime / (mode + ".log"), "ab")
        logs.append(log)
        args = [str(BINARY), mode, "--data-dir", str(runtime), "--socket-dir", str(runtime)]
        if mode == "web":
            args += ["--listen", f"127.0.0.1:{web_port}"]
        proc = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=log, stderr=log)
        processes.append(proc)
        return proc

    def persisted(run_id):
        with sqlite3.connect(runtime / "app.db") as db:
            row = db.execute("SELECT offset FROM run_offsets WHERE run_id=?", (run_id,)).fetchone()
            return None if row is None else row[0]

    def replication_offset():
        return int(next(line.split(":")[1] for line in redis(ports[0], "INFO", "replication").splitlines() if line.startswith("master_repl_offset:")))

    try:
        ports = []
        for role in ["source", "target"]:
            name = f"offset-e2e-{token}-{role}"
            subprocess.run(["docker", "run", "--rm", "-d", "--name", name, "-p", "127.0.0.1::6379", "redis:7.2", "redis-server", "--save", "", "--appendonly", "no", "--repl-diskless-sync", "no"], check=True, capture_output=True)
            containers.append(name)
            port = int(subprocess.check_output(["docker", "port", name, "6379/tcp"], text=True).strip().split(":")[-1])
            ports.append(port)
            wait_for(lambda: redis(port, "PING") == "PONG")
        redis(ports[0], "SET", "snapshot", "full-sync-value")
        runner = start("runner")
        wait_for(lambda: (runtime / "runner.sock").exists())
        web = start("web")

        def web_ready():
            try:
                return api("/bootstrap/status")
            except (OSError, ValueError):
                return False

        wait_for(web_ready)
        api("/bootstrap/init", {"username": "offset_test", "password": "isolated-offset-test"})
        csrf = api("/login", {"username": "offset_test", "password": "isolated-offset-test"})["csrf"]
        connections = [api("/connections", {"name": role, "kind": "standalone", "address": f"127.0.0.1:{port}", "authMode": "none"}) for role, port in zip(["source", "target"], ports)]
        draft = {"name": "Disposable offset integration", "sourceId": connections[0]["id"], "targetId": connections[1]["id"], "dbMap": {"0": 0}, "rules": {"blockPrefixes": ["excluded:"]}, "targetPolicy": "require_empty"}
        checks = api('/tasks/preflight', draft)
        assert all(check["ok"] for check in checks), checks
        assert api('/tasks') == [], "preview must not create a task"
        assert redis(ports[1], "DBSIZE") == "0", "preview must not modify the target"
        task = api("/tasks", draft)
        checks = api(f'/tasks/{task["id"]}/preflight', {})
        assert all(check["ok"] for check in checks), checks
        run = api(f'/tasks/{task["id"]}/start', {})
        run_id = run["id"]
        wait_for(lambda: redis(ports[1], "GET", "snapshot") == "full-sync-value")
        wait_for(lambda: persisted(run_id) is not None)
        baseline = persisted(run_id)
        assert baseline >= 0
        redis(ports[0], "SET", "incremental", "first")
        expected = replication_offset()
        wait_for(lambda: redis(ports[1], "GET", "incremental") == "first")
        wait_for(lambda: (persisted(run_id) or 0) >= expected)
        first = persisted(run_id)
        # Write immediately after a periodic save, then stop before the next tick.
        redis(ports[0], "SET", "incremental", "final")
        redis(ports[0], "SET", "excluded:key", "filtered")
        expected_final = replication_offset()
        wait_for(lambda: redis(ports[1], "GET", "incremental") == "final")
        # Allow the adjacent filtered command to pass through the reader.
        time.sleep(0.15)
        assert redis(ports[1], "GET", "excluded:key") is None
        assert persisted(run_id) == first, "commands must remain in memory until flush"
        api(f"/runs/{run_id}/stop", {})
        runs_path = f'/tasks/{task["id"]}/runs'
        wait_for(lambda: api(runs_path)[0]["status"] == "STOPPED")
        saved = persisted(run_id)
        assert saved >= expected_final, (saved, expected_final)
        assert api(runs_path)[0]["consumedOffsets"] == [{"node": f"127.0.0.1:{ports[0]}", "offset": str(saved)}]
        subprocess.run([str(BINARY), "shutdown", "--socket-dir", str(runtime)], check=True, capture_output=True)
        runner.wait(timeout=15)
        runner = start("runner")
        wait_for(web_ready)
        assert api(runs_path)[0]["consumedOffsets"][0]["offset"] == str(saved)
        print(f"PASS: snapshot ACK, incremental ACK, in-memory buffering, periodic SQLite save, filtered commands, stop flush, API strings, runner restart. Offsets {baseline} -> {first} -> {saved}.")
    finally:
        subprocess.run([str(BINARY), "shutdown", "--socket-dir", str(runtime)], capture_output=True, timeout=25)
        for proc in reversed(processes):
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=20)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()
        for name in containers:
            subprocess.run(["docker", "rm", "-f", name], capture_output=True)
        for log in logs:
            log.close()


if __name__ == "__main__":
    main()
