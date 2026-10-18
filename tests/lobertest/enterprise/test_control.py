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


from service_fixture import EnterpriseServiceFixture, ROLES


class ControlTests(EnterpriseServiceFixture, unittest.TestCase):
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
        completed = subprocess.run(command, input=json.dumps({"url": self.url, "password": self.password, "failureScreenshot": str(self.work / "ui-failure.png")}), capture_output=True, text=True, timeout=150)
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
        # Registration rejects the path synchronously, after persisting the ISO
        # identity. No source may be queued or become ready after that rejection.
        result = self.assert_status(self.admin("POST", "/isos", {"name": "outside-root", "family": "linux", "series": "rocky", "sources": [{"kind": "path", "addr": "/etc/passwd"}]}), 422)
        self.assertTrue(any(problem["code"] == "iso_source_rejected" and "本地路径" in problem["message"] for problem in result["problems"]))
        page = self.assert_status(self.admin("GET", "/isos?limit=100"), 200)
        rows = [item for item in page["items"] if item["name"] == "outside-root"]
        self.assertEqual(len(rows), 1)
        progress = self.assert_status(self.admin("GET", "/isos/" + rows[0]["id"] + "/progress"), 200)
        self.assertNotEqual(progress["state"], "ready")
        self.assertEqual(progress["sources"], [])

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
        before = self.assert_status(self.admin("GET", "/isos"), 200)
        self.assert_status(self.admin("POST", "/isos", {"name": "invalid-family-series", "family": "windows", "series": "rocky", "sources": [{"kind": "upload", "size": 100}]}), 400)
        self.assertEqual(self.assert_status(self.admin("GET", "/isos"), 200), before)

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
        env = postgres_env(self.base_dsn)
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
