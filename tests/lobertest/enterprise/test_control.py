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


ROLES = ("admin", "operator", "hook-author", "hook-approver", "viewer")


class ControlTests(unittest.TestCase):
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
        cls.config.write_text(json.dumps({"listen": f"127.0.0.1:{cls.port}",
            "pg": {"dsn": dsn, "maxConns": 8}, "seedAdmin": cls.password,
            "log": {"level": "error"},
            "iso": {"staging": str(cls.work / "staging"),
                    "artifacts": str(cls.work / "artifacts"), "smb": str(cls.work / "smb"),
                    "tools": str(cls.work / "tools"), "localRoots": [str(cls.work)],
                    "workers": 1, "pollMS": 50}}))
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
    def db(cls, sql):
        # Credentials remain in the child environment, never command-line logs.
        env = os.environ.copy()
        env["PGDATABASE"] = cls.base_dsn
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
            cls.proc.wait(timeout=25)
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
        return {"isoID": item["id"], "templateID": template["id"], "templateVersion": 1}

    def test_batch_atomicity_snapshot_freeze_and_unique_open_task(self):
        selection = self.selection()
        first, second = self.host(), self.host()
        submission = {**selection, "hosts": [{"sn": first["sn"], "entered": {"tpl/hostname": "node-one"}}, {"sn": second["sn"], "entered": {"tpl/hostname": "node-two"}}]}
        invalid = {**selection, "hosts": [submission["hosts"][0], {"sn": "NOT-REGISTERED"}]}
        self.assert_status(self.admin("POST", "/wizard/tasks", invalid), 422)
        self.assertEqual(self.assert_status(self.admin("GET", "/tasks?host_sn=" + first["sn"]), 200)["items"], [])
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            responses = list(pool.map(lambda _: self.admin("POST", "/wizard/tasks", submission), range(2)))
        self.assertEqual(sum(response[0] == 200 for response in responses), 1)
        rejected = next(response for response in responses if response[0] != 200)
        self.assertIn(rejected[0], (409, 422))
        tasks = next(response[1]["data"]["tasks"] for response in responses if response[0] == 200)
        self.assertEqual(len(tasks), 2)
        for host, task in zip((first, second), tasks):
            self.assertEqual(task["state"], "pending")
            self.assertEqual(len(self.assert_status(self.admin("GET", "/tasks?host_sn=" + host["sn"]), 200)["items"]), 1)
            frozen = self.assert_status(self.admin("GET", "/tasks/" + task["id"]), 200)
            self.assert_status(self.admin("PUT", "/hosts/" + host["sn"], {**host, "attrs": {"rack": "changed-after-task"}}), 200)
            self.assertEqual(self.assert_status(self.admin("GET", "/tasks/" + task["id"]), 200)["snapshot"], frozen["snapshot"])
            preview = self.assert_status(self.admin("POST", "/tasks/" + task["id"] + "/preview", {"firmware": "uefi"}), 200)
            self.assertTrue(any(file["name"] == "ks.cfg" for file in preview["files"]))

    def test_anonymous_cannot_read(self):
        for path in ("/hosts", "/isos", "/tasks", "/hooks", "/templates", "/audit", "/users", "/roles/menus"):
            with self.subTest(path=path):
                self.assert_status(self.request("GET", path), 401)

    def test_bad_login_does_not_issue_token(self):
        self.assert_status(self.request("POST", "/auth/login", body={"username": "missing", "password": self.password}), 401)

    def test_browser_production_pages_and_independent_viewer_context(self):
        command = [os.environ["LOBER_ENTERPRISE_NODE"], os.environ["LOBER_ENTERPRISE_UI_SCRIPT"], os.environ["LOBER_ENTERPRISE_PLAYWRIGHT"]]
        completed = subprocess.run(command, input=json.dumps({"url": self.url, "password": self.password}), capture_output=True, text=True, timeout=150)
        if completed.returncode:
            self.fail("Production browser checks failed: " + completed.stderr[-1800:])
        output = json.loads(completed.stdout.strip())
        self.assertEqual(output["status"], "PASS")
        self.assertEqual(output["count"], 4)

    def test_host_update_conflict_preserves_winner(self):
        host = self.host()
        first = self.assert_status(self.admin("PUT", "/hosts/" + host["sn"], {**host, "attrs": {"rack": "winner"}}), 200)
        self.assert_status(self.admin("PUT", "/hosts/" + host["sn"], {**host, "attrs": {"rack": "stale"}}), 409)
        stored = self.assert_status(self.admin("GET", "/hosts/" + host["sn"]), 200)
        self.assertEqual(stored, first)

    def test_concurrent_same_sn_has_one_winner(self):
        sn = "RACE-" + secrets.token_hex(5).upper()
        with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
            responses = list(pool.map(lambda _: self.admin("POST", "/hosts", {"sn": sn, "attrs": {}}), range(6)))
        self.assertEqual(sorted(r[0] for r in responses), [200, 409, 409, 409, 409, 409])
        rows = self.assert_status(self.admin("GET", "/hosts?prefix=" + sn), 200)["items"]
        self.assertEqual(len(rows), 1)

    def test_csv_preview_has_no_writes(self):
        sn = "CSV-" + secrets.token_hex(5).upper()
        csv = f"sn,rack\n{sn},preview\n".encode()
        self.assert_status(self.admin("POST", "/hosts/import?dryRun=true", raw=csv, headers={"Content-Type": "text/csv"}), 200)
        self.assert_status(self.admin("GET", "/hosts/" + sn), 404)
        self.assert_status(self.admin("POST", "/hosts/import", raw=csv, headers={"Content-Type": "text/csv"}), 200)
        self.assertEqual(self.assert_status(self.admin("GET", "/hosts/" + sn), 200)["attrs"]["rack"], "preview")

    def test_invalid_csv_batch_is_atomic(self):
        sn = "ATOMIC-" + secrets.token_hex(5).upper()
        csv = f"sn,rack\n{sn},valid\n,invalid\n".encode()
        self.assert_status(self.admin("POST", "/hosts/import", raw=csv, headers={"Content-Type": "text/csv"}), 422)
        self.assert_status(self.admin("GET", "/hosts/" + sn), 404)

    def test_invalid_pagination_is_rejected(self):
        for limit in ("-1", "501", "invalid"):
            with self.subTest(limit=limit):
                self.assert_status(self.admin("GET", "/hosts?limit=" + limit), 400)

    def test_menu_cas_and_empty_menu_do_not_grant_write_access(self):
        role_data = self.assert_status(self.admin("GET", "/roles/menus"), 200)
        viewer = next(item for item in role_data["roles"] if item["role"] == "viewer")
        saved = self.assert_status(self.admin("PUT", "/roles/viewer/menus", {"menuIds": [], "rev": viewer["rev"]}), 200)
        self.assert_status(self.admin("PUT", "/roles/viewer/menus", {"menuIds": ["hosts"], "rev": viewer["rev"]}), 409)
        self.assert_status(self.request("POST", "/hosts", self.tokens["viewer"], {"sn": "DENIED", "attrs": {}}), 403)
        self.assert_status(self.request("GET", "/user/info", self.tokens["viewer"]), 200)
        self.assert_status(self.admin("PUT", "/roles/viewer/menus", {"menuIds": viewer["menuIds"], "rev": saved["rev"]}), 200)

    def test_profile_stale_revision_and_security_field_injection(self):
        token = self.tokens["viewer"]
        user = self.assert_status(self.request("GET", "/user/info", token), 200)
        body = {"displayName": "Enterprise Viewer", "email": "viewer@example.invalid", "avatar": "", "bio": "测试资料", "rev": user["rev"], "role": "admin", "disabled": True}
        self.assert_status(self.request("PUT", "/user/profile", token, body), 200)
        self.assert_status(self.request("PUT", "/user/profile", token, body), 409)
        current = self.assert_status(self.request("GET", "/user/info", token), 200)
        self.assertEqual(current["role"], "viewer")
        self.assertEqual(current["displayName"], "Enterprise Viewer")
        self.assert_status(self.request("GET", "/users", token), 403)

    def test_logout_revokes_only_issued_session(self):
        token = self.login("enterprise_viewer", self.password)
        self.assert_status(self.request("POST", "/auth/logout", token), 200)
        self.assert_status(self.request("GET", "/user/info", token), 401)
        self.assert_status(self.request("GET", "/user/info", self.tokens["viewer"]), 200)

    def test_password_change_revokes_all_old_sessions(self):
        username = "pw_" + secrets.token_hex(5)
        self.assert_status(self.admin("POST", "/users", {"username": username, "role": "viewer", "password": self.password}), 200)
        first = self.login(username, self.password)
        second = self.login(username, self.password)
        replacement = secrets.token_urlsafe(25)
        self.assert_status(self.request("POST", "/auth/password", first, {"currentPassword": self.password, "newPassword": replacement}), 200)
        for token in (first, second):
            self.assert_status(self.request("GET", "/user/info", token), 401)
        self.assert_status(self.request("POST", "/auth/login", body={"username": username, "password": self.password}), 401)
        self.assert_status(self.request("GET", "/user/info", self.login(username, replacement)), 200)

    def test_disabled_account_cannot_reuse_issued_session(self):
        username = "disable_" + secrets.token_hex(4)
        self.assert_status(self.admin("POST", "/users", {"username": username, "role": "viewer", "password": self.password}), 200)
        token = self.login(username, self.password)
        self.assert_status(self.admin("DELETE", "/users/" + username), 200)
        self.assert_status(self.request("GET", "/user/info", token), 401)
        self.assert_status(self.request("POST", "/auth/login", body={"username": username, "password": self.password}), 401)

    def test_last_admin_cannot_disable_self(self):
        self.assert_status(self.admin("DELETE", "/users/admin"), 409)
        self.assert_status(self.admin("GET", "/users"), 200)

    def test_role_change_applies_to_already_issued_session(self):
        username = "role_" + secrets.token_hex(4)
        user = self.assert_status(self.admin("POST", "/users", {"username": username, "role": "operator", "password": self.password}), 200)
        token = self.login(username, self.password)
        self.assert_status(self.request("POST", "/hosts", token, {"sn": "ROLE-" + secrets.token_hex(4).upper(), "attrs": {}}), 200)
        self.assert_status(self.admin("PUT", "/users/" + username, {"displayName": "demoted", "role": "viewer", "rev": user["rev"]}), 200)
        self.assert_status(self.request("POST", "/hosts", token, {"sn": "DENIED-" + secrets.token_hex(4).upper(), "attrs": {}}), 403)

    def test_local_source_outside_allowed_root_never_ready(self):
        item = self.assert_status(self.admin("POST", "/isos", {"name": "outside-root", "family": "linux", "series": "rocky", "sources": [{"kind": "path", "addr": "/etc/passwd"}]}), 200)
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            progress = self.assert_status(self.admin("GET", "/isos/" + item["id"] + "/progress"), 200)
            self.assertNotEqual(progress["state"], "ready")
            if progress["state"] == "failed":
                self.assertTrue(any(source["error"] for source in progress["sources"]))
                return
            time.sleep(.1)
        self.fail("Out-of-root source was not rejected with a persistent reason")

    def test_upload_missing_range_is_rejected_and_invalid_media_never_ready(self):
        payload = b"This is deliberately not a valid installation ISO."
        item = self.assert_status(self.admin("POST", "/isos", {"name": "enterprise-invalid", "family": "linux", "series": "rocky", "sources": [{"kind": "upload", "size": len(payload), "sha256": hashlib.sha256(payload).hexdigest()}]}), 200)
        path = "/isos/" + item["id"]
        self.assert_status(self.admin("PUT", path + "/content", raw=payload, headers={"Content-Type": "application/octet-stream"}), 400)
        self.assert_status(self.admin("PUT", path + "/content", raw=payload, headers={"Content-Type": "application/octet-stream", "Content-Range": f"bytes 0-{len(payload)-1}/{len(payload)}"}), 200)
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            progress = self.assert_status(self.admin("GET", path + "/progress"), 200)
            self.assertNotEqual(progress["state"], "ready")
            if progress["state"] == "failed":
                self.assertTrue(any(source["error"] for source in progress["sources"]))
                return
            time.sleep(.1)
        self.fail("Invalid uploaded media did not reach a diagnosed failed state")

    def test_invalid_iso_family_series_is_rejected(self):
        self.assert_status(self.admin("POST", "/isos", {"family": "windows", "series": "rocky", "sources": [{"kind": "upload", "size": 100}]}), 422)

    def test_unusable_iso_cannot_create_any_tasks(self):
        host = self.host()
        before = self.assert_status(self.admin("GET", "/tasks"), 200)
        self.assert_status(self.admin("POST", "/wizard/tasks", {"isoID": "missing", "templateID": "missing", "templateVersion": 1, "hosts": [{"sn": host["sn"]}, {"sn": "UNKNOWN"}]}), 404)
        after = self.assert_status(self.admin("GET", "/tasks"), 200)
        self.assertEqual(before, after)

    def test_audit_contains_host_creation(self):
        host = self.host()
        data = self.assert_status(self.admin("GET", "/audit?limit=500"), 200)
        self.assertTrue(any(item.get("targetID") == host["sn"] and item.get("action") == "host.create" for item in data["items"]))

    def test_zy_database_and_artifacts_backup_restore(self):
        host = self.host()
        expected_profile = self.assert_status(self.request("GET", "/user/info", self.tokens["viewer"]), 200)
        self.proc.terminate()
        self.proc.wait(timeout=25)
        env = os.environ.copy()
        env["PGDATABASE"] = self.base_dsn
        dump = self.work / "database.sql"
        backup = self.work / "files-backup"
        backup.mkdir()
        directories = ("staging", "artifacts", "smb")
        def manifest(root):
            return {str(path.relative_to(root)): hashlib.sha256(path.read_bytes()).hexdigest()
                    for path in root.rglob("*") if path.is_file()}
        expected_files = {}
        try:
            dumped = subprocess.run(["pg_dump", "--no-owner", "--no-privileges", "--schema=" + self.schema, "--file=" + str(dump)], env=env, capture_output=True, timeout=60)
            dump.chmod(0o600)
            self.assertEqual(dumped.returncode, 0, "Schema backup failed; restricted output suppressed")
            for name in directories:
                directory = self.work / name
                self.assertEqual(directory.resolve().parent, self.work.resolve())
                if directory.exists():
                    expected_files[name] = manifest(directory)
                    shutil.copytree(directory, backup / name, symlinks=True)
                    shutil.rmtree(directory)
            self.db("DROP SCHEMA " + self.schema + " CASCADE")
            restored = subprocess.run(["psql", "-X", "-v", "ON_ERROR_STOP=1", "-f", str(dump)], env=env, capture_output=True, timeout=60)
            self.assertEqual(restored.returncode, 0, "Schema restore failed; restricted output suppressed")
            for name, expected in expected_files.items():
                shutil.copytree(backup / name, self.work / name, symlinks=True)
                self.assertEqual(manifest(self.work / name), expected)
        finally:
            dump.unlink(missing_ok=True)
        self.start()
        for role in ROLES:
            self.tokens[role] = self.login("admin" if role == "admin" else "enterprise_" + role.replace("-", "_"), self.password)
        self.assertEqual(self.assert_status(self.admin("GET", "/hosts/" + host["sn"]), 200), host)
        current = self.assert_status(self.request("GET", "/user/info", self.tokens["viewer"]), 200)
        self.assertEqual(current["displayName"], expected_profile["displayName"])

    def test_zz_process_restart_preserves_data_and_revokes_sessions(self):
        host = self.host()
        expected_profile = self.assert_status(self.request("GET", "/user/info", self.tokens["viewer"]), 200)
        self.proc.kill()
        self.proc.wait(timeout=10)
        self.start()
        self.assert_status(self.admin("GET", "/hosts"), 401)
        for role in ROLES:
            self.tokens[role] = self.login("admin" if role == "admin" else "enterprise_" + role.replace("-", "_"), self.password)
        self.assertEqual(self.assert_status(self.admin("GET", "/hosts/" + host["sn"]), 200), host)
        current = self.assert_status(self.request("GET", "/user/info", self.tokens["viewer"]), 200)
        self.assertEqual(current["displayName"], expected_profile["displayName"])


def permission_case(role, method, path, allowed):
    def test(self):
        response = self.request(method, path, self.tokens[role], {} if method != "GET" else None)
        if allowed:
            if method == "GET":
                self.assert_status(response, 200)
            else:
                # Deliberately malformed writes must reach handler validation.
                self.assertIn(response[0], (200, 400, 422))
        else:
            self.assert_status(response, 403)
    return test


for _role in ROLES:
    for _method, _path, _allowed_roles in (
        ("GET", "/hosts", ROLES), ("GET", "/isos", ROLES),
        ("GET", "/hooks", ROLES), ("GET", "/templates", ROLES),
        ("GET", "/tasks", ROLES), ("GET", "/users", ("admin",)),
        ("GET", "/roles/menus", ("admin",)), ("GET", "/audit", ("admin",)),
        ("POST", "/hosts", ("admin", "operator")),
        ("POST", "/isos", ("admin",)),
        ("POST", "/hooks", ("admin", "hook-author")),
        ("POST", "/templates", ("admin", "hook-author")),
        ("POST", "/wizard/tasks", ("admin", "operator")),
    ):
        _name = "test_permission_" + _role.replace("-", "_") + "_" + _method.lower() + _path.replace("/", "_")
        setattr(ControlTests, _name, permission_case(_role, _method, _path, _role in _allowed_roles))


if __name__ == "__main__":
    unittest.main()
