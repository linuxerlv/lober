"""Current-rewrite HTTP checks against a dedicated real Linux/PostgreSQL service.

No legacy client, route aliases, credentials or direct business-table writes.
Use the runner documented in README; missing configuration is an error, not skip.
"""
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import tempfile
import time
import unittest
import urllib.error
import urllib.parse
import urllib.request

from db_env import postgres_env


ROLES = ("admin", "operator", "hook-author", "hook-approver", "viewer")


class EnterpriseServiceFixture:
    @classmethod
    def setUpClass(cls):
        cls.base_dsn = os.environ["LOBER_TEST_PG_DSN"]
        cls.binary = Path(os.environ["LOBER_ENTERPRISE_BINARY"]).resolve(strict=True)
        cls.work = Path(tempfile.mkdtemp(prefix="lober-enterprise-http-"))
        cls.schema = "enterprise_" + secrets.token_hex(8)
        cls.proc = None
        cls.log = None
        cls.config = None
        cls.db("CREATE SCHEMA " + cls.schema)
        cls.addClassCleanup(cls.cleanup)
        if "://" in cls.base_dsn:
            url = urllib.parse.urlsplit(cls.base_dsn)
            query = dict(urllib.parse.parse_qsl(url.query))
            query["search_path"] = cls.schema
            dsn = urllib.parse.urlunsplit(url._replace(query=urllib.parse.urlencode(query)))
        else:
            dsn = cls.base_dsn + " search_path=" + cls.schema
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            cls.port = sock.getsockname()[1]
        cls.url = "http://127.0.0.1:" + str(cls.port)
        cls.password = secrets.token_urlsafe(25)
        cls.config = cls.work / "config.json"
        config = {"listen": f"127.0.0.1:{cls.port}",
            "pg": {"dsn": dsn, "maxConns": 8}, "seedAdmin": cls.password,
            "log": {"level": "error"},
            "iso": {"staging": str(cls.work / "staging"),
                    "artifacts": str(cls.work / "artifacts"), "smb": str(cls.work / "smb"),
                    "tools": str(cls.work / "tools"), "localRoots": [str(cls.work)],
                    "workers": 1, "pollMS": 50}}
        cls.configure(config)
        cls.config.write_text(json.dumps(config))
        cls.config.chmod(0o600)
        cls.log = (cls.work / "service.log").open("ab")
        cls.start()
        bootstrap = cls.login("admin", cls.password)
        cls.assert_status(cls.request("GET", "/hosts", bootstrap), 403)
        new_password = secrets.token_urlsafe(25)
        cls.assert_status(cls.request("POST", "/auth/password", bootstrap,
            {"currentPassword": cls.password, "newPassword": new_password}), 200)
        cls.assert_status(cls.request("GET", "/user/info", bootstrap), 401)
        cls.password = new_password
        cls.tokens = {"admin": cls.login("admin", cls.password)}
        for role in ROLES[1:]:
            cls.assert_status(cls.request("POST", "/users", cls.tokens["admin"],
                {"username": "enterprise_" + role.replace("-", "_"), "displayName": role,
                 "role": role, "password": cls.password}), 200)
            cls.tokens[role] = cls.login("enterprise_" + role.replace("-", "_"), cls.password)

    @classmethod
    def configure(cls, config):
        """Allow isolated fault fixtures to replace only their own dependencies."""

    @classmethod
    def db(cls, sql):
        # Credentials remain in the child environment, never command-line logs.
        env = postgres_env(cls.base_dsn)
        done = subprocess.run(["psql", "-X", "-v", "ON_ERROR_STOP=1", "-At", "-c", sql],
            env=env, capture_output=True, text=True, timeout=30)
        if done.returncode:
            raise RuntimeError("Dedicated test database operation failed (credentials suppressed)")
        return done.stdout.strip()

    @classmethod
    def start(cls):
        env = os.environ.copy()
        env.pop("LOBER_PG_DSN", None)
        env["GIN_MODE"] = "release"
        cls.proc = subprocess.Popen([str(cls.binary), "-config", str(cls.config)],
            cwd=cls.work, env=env, stdout=cls.log, stderr=cls.log)
        for _ in range(150):
            if cls.proc.poll() is not None:
                raise RuntimeError("Candidate service exited during startup; inspect restricted service.log")
            try:
                if cls.request("POST", "/auth/login", body={})[0] == 400:
                    return
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(.1)
        raise RuntimeError("Candidate service did not become ready within 15 seconds")

    @classmethod
    def cleanup(cls):
        if cls.proc and cls.proc.poll() is None:
            cls.proc.terminate()
            try:
                cls.proc.wait(timeout=25)
            except subprocess.TimeoutExpired:
                cls.proc.kill()
                cls.proc.wait(timeout=10)
        if cls.log:
            cls.log.close()
        if cls.config:
            cls.config.unlink(missing_ok=True)
        cls.db("DROP SCHEMA IF EXISTS " + cls.schema + " CASCADE")

    @classmethod
    def request(cls, method, path, token=None, body=None, raw=None, headers=None):
        heads = {"Content-Type": "application/json", **(headers or {})}
        if token:
            heads["Authorization"] = "Bearer " + token
        data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
        req = urllib.request.Request(cls.url + "/console/v1" + path, data=data, method=method, headers=heads)
        try:
            response = urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req, timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, json.loads(response.read())

    @classmethod
    def assert_status(cls, response, expected):
        # Do not include response bodies: they can contain an authentication token.
        if response[0] != expected or response[1].get("code") != (0 if expected == 200 else expected):
            raise AssertionError(f"Expected HTTP/envelope {expected}, received {response[0]}/{response[1].get('code')}")
        return response[1].get("data")

    @classmethod
    def login(cls, username, password):
        data = cls.assert_status(cls.request("POST", "/auth/login", body={"username": username, "password": password}), 200)
        if not isinstance(data.get("token"), str) or not data["token"]:
            raise AssertionError("Current login contract did not return token")
        return data["token"]

    def admin(self, method, path, body=None, **kwargs):
        return self.request(method, path, self.tokens["admin"], body, **kwargs)

    def host(self, sn=None):
        sn = sn or "ENTERPRISE-" + secrets.token_hex(5).upper()
        return self.assert_status(self.admin("POST", "/hosts", {"sn": sn, "attrs": {"rack": "test"}}), 200)

    def selection(self):
        # This is explicitly synthetic integration media, not real-install evidence.
        payload = Path(os.environ["LOBER_ENTERPRISE_FIXTURE"]).read_bytes()
        item = self.assert_status(self.admin("POST", "/isos", {"name": "synthetic-integration-only", "family": "linux", "series": "rocky", "sources": [{"kind": "upload", "size": len(payload), "sha256": hashlib.sha256(payload).hexdigest()}]}), 200)
        path = "/isos/" + item["id"]
        self.assert_status(self.admin("PUT", path + "/content", raw=payload, headers={"Content-Type": "application/octet-stream", "Content-Range": f"bytes 0-{len(payload)-1}/{len(payload)}"}), 200)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            current = self.assert_status(self.admin("GET", path), 200)
            if current["state"] == "ready":
                break
            self.assertNotEqual(current["state"], "failed", "Synthetic fixture import failed")
            time.sleep(.1)
        else:
            self.fail("Synthetic integration fixture did not import within budget")
        return {"isoID": item["id"], **self.approved_template()}

    def approved_template(self):
        author = self.tokens["hook-author"]
        template = self.assert_status(self.request("POST", "/templates", author, {
            "name": "enterprise_" + secrets.token_hex(4), "family": "linux", "series": ["rocky"],
            "body": "# {{param:hostname}}\n{{lober:syslog}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n",
            "params": [{"name": "hostname", "label": "hostname", "type": "hostname", "default": "node"}]}), 200)
        version_path = "/templates/" + template["id"] + "/versions/1"
        review = self.assert_status(self.request("POST", version_path + "/submit?rev=" + str(template["rev"]), author), 200)
        self.assert_status(self.request("POST", version_path + "/approve?rev=" + str(review["rev"]), author), 403)
        approved = self.assert_status(self.request("POST", version_path + "/approve?rev=" + str(review["rev"]), self.tokens["hook-approver"]), 200)
        self.assertEqual(approved["state"], "approved")
        return {"templateID": template["id"], "templateVersion": 1}
