#!/usr/bin/env python3
"""ComposeDrill deterministic local release gate for macOS.

Run from the repository root:

    python3 dev.py

The harness does not install software, change ComposeDrill behavior, or weaken the
safety model. It exercises the existing release gate against a real local Docker
daemon and writes redacted evidence under ./artifacts/.
"""

from __future__ import print_function

import datetime as _dt
import hashlib
import json
import os
import platform
import re
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent
ARTIFACTS = ROOT / "artifacts"
TEXT_REPORT = ARTIFACTS / "local_verification_report.txt"
JSON_REPORT = ARTIFACTS / "local_verification_report.json"
FULL_LOG = ARTIFACTS / "full_validation.log"
BINARY = ARTIFACTS / "composedrill-validation-bin"

EXPECTED_INTEGRATION_TESTS = {
    "TestStatelessRecovery",
    "TestBrokenBackupDetected",
    "TestPostgresLogicalRestore",
    "TestSQLiteRecovery",
    "TestBindMountedRecovery",
    "TestNamedVolumeArchiveRecovery",
    "TestAnonymousVolumeArchiveRecovery",
    "TestMariaDBLogicalRestore",
}

SUCCESS_FIXTURES = ("stateless", "postgres", "sqlite", "bind")
SENSITIVE_KEY_RE = re.compile(
    r"(?i)(pass(word|wd)?|secret|token|api[_-]?key|private[_-]?key|credential|cookie|authorization)"
)
SENSITIVE_ASSIGNMENT_RE = re.compile(
    r"(?i)(\b[A-Za-z0-9_.-]*(?:pass(?:word|wd)?|secret|token|api[_-]?key|private[_-]?key|credential|cookie|authorization)[A-Za-z0-9_.-]*\b"
    r"\s*[=:]\s*)([^\s,;]+)"
)
SENSITIVE_JSON_RE = re.compile(
    r'(?i)("[^"]*(?:pass(?:word|wd)?|secret|token|api[_-]?key|private[_-]?key|credential|cookie|authorization)[^"]*"\s*:\s*")([^"]*)(")'
)
URL_CREDENTIAL_RE = re.compile(r"(https?://[^\s/:@]+:)([^\s/@]+)(@)", re.IGNORECASE)


class Harness:
    def __init__(self):
        ARTIFACTS.mkdir(parents=True, exist_ok=True)
        self.log_fp = FULL_LOG.open("w", encoding="utf-8")
        self.started = _dt.datetime.now(_dt.timezone.utc)
        self.commands = []
        self.notes = []
        self.prerequisites = []
        self.rehearsals = {}
        self.cleanup = {}
        self.production_isolation = {}
        self.known_secret_values = self._discover_known_secret_values()
        self.matrix = {
            "GO TOOLCHAIN": "NOT RUN",
            "DOCKER ENGINE": "NOT RUN",
            "DOCKER COMPOSE": "NOT RUN",
            "UNIT TESTS": "NOT RUN",
            "RACE TESTS": "NOT RUN",
            "GO VET": "NOT RUN",
            "BUILD": "NOT RUN",
            "INTEGRATION SUITE": "NOT RUN",
            "STATELESS RESTORE": "NOT RUN",
            "POSTGRES RESTORE": "NOT RUN",
            "SQLITE RESTORE": "NOT RUN",
            "BIND RESTORE": "NOT RUN",
            "BROKEN FIXTURE": "NOT RUN",
            "CLEANUP": "NOT RUN",
            "PRODUCTION ISOLATION": "NOT RUN",
            "RELEASE GATE": "NOT RUN",
        }

    def close(self):
        try:
            self.log_fp.flush()
            self.log_fp.close()
        except Exception:
            pass

    def log(self, text=""):
        text = self.redact(str(text))
        print(text)
        self.log_fp.write(text + "\n")
        self.log_fp.flush()

    def redact(self, text):
        if text is None:
            return ""
        out = str(text)
        for value in sorted(self.known_secret_values, key=len, reverse=True):
            if value and len(value) >= 4:
                out = out.replace(value, "[REDACTED]")
        out = SENSITIVE_JSON_RE.sub(r'\1[REDACTED]\3', out)
        out = SENSITIVE_ASSIGNMENT_RE.sub(r"\1[REDACTED]", out)
        out = URL_CREDENTIAL_RE.sub(r"\1[REDACTED]\3", out)
        return out

    def _discover_known_secret_values(self):
        values = set()
        # Fixture .env files are intentionally part of the test corpus, but their
        # values should still never appear in validation artifacts.
        for path in (ROOT / "examples").rglob(".env") if (ROOT / "examples").exists() else []:
            try:
                for raw in path.read_text(encoding="utf-8", errors="replace").splitlines():
                    line = raw.strip()
                    if not line or line.startswith("#") or "=" not in line:
                        continue
                    key, value = line.split("=", 1)
                    value = value.strip().strip('"').strip("'")
                    if SENSITIVE_KEY_RE.search(key) and value:
                        values.add(value)
            except OSError:
                pass
        # Environment values are not printed by this harness, but if a tool echoes
        # one associated with a sensitive key, redact it from captured output.
        for key, value in os.environ.items():
            if SENSITIVE_KEY_RE.search(key) and value:
                values.add(value)
        return values

    def run(self, args, label, timeout=900, cwd=ROOT, env=None):
        args = [str(x) for x in args]
        shown = " ".join(shell_quote(x) for x in args)
        self.log("\n=== {} ===".format(label))
        self.log("$ {}".format(shown))
        start = time.monotonic()
        timed_out = False
        output = ""
        exit_code = None
        run_env = os.environ.copy() if env is None else dict(env)
        try:
            proc = subprocess.Popen(
                args,
                cwd=str(cwd),
                env=run_env,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                encoding="utf-8",
                errors="replace",
                start_new_session=True,
            )
            try:
                output, _ = proc.communicate(timeout=timeout)
            except subprocess.TimeoutExpired:
                timed_out = True
                self.log("Command exceeded {} seconds; sending SIGINT to its process group.".format(timeout))
                try:
                    os.killpg(proc.pid, signal.SIGINT)
                except (ProcessLookupError, PermissionError):
                    pass
                try:
                    output, _ = proc.communicate(timeout=20)
                except subprocess.TimeoutExpired:
                    try:
                        os.killpg(proc.pid, signal.SIGKILL)
                    except (ProcessLookupError, PermissionError):
                        pass
                    output, _ = proc.communicate()
            exit_code = proc.returncode
        except FileNotFoundError as exc:
            output = str(exc)
            exit_code = 127
        except Exception as exc:
            output = "harness subprocess error: {}".format(exc)
            exit_code = 126
        duration = round(time.monotonic() - start, 3)
        safe_output = self.redact(output.rstrip())
        if safe_output:
            self.log(safe_output)
        self.log("[exit_code={}; duration_seconds={}; timed_out={}]".format(exit_code, duration, str(timed_out).lower()))
        record = {
            "label": label,
            "command": args,
            "display_command": shown,
            "exit_code": exit_code,
            "duration_seconds": duration,
            "timed_out": timed_out,
            "output": safe_output,
        }
        self.commands.append(record)
        return record

    def note(self, text):
        self.notes.append(self.redact(text))
        self.log("NOTE: " + text)

    def prerequisite(self, text):
        self.prerequisites.append(self.redact(text))
        self.log("PREREQUISITE: " + text)

    def write_reports(self):
        finished = _dt.datetime.now(_dt.timezone.utc)
        result = {
            "schema_version": 1,
            "project": "ComposeDrill",
            "started_at": self.started.isoformat(),
            "finished_at": finished.isoformat(),
            "duration_seconds": round((finished - self.started).total_seconds(), 3),
            "repository_root": str(ROOT),
            "platform": {
                "system": platform.system(),
                "release": platform.release(),
                "machine": platform.machine(),
                "python": platform.python_version(),
                "python_executable": sys.executable,
            },
            "matrix": self.matrix,
            "prerequisites": self.prerequisites,
            "notes": self.notes,
            "rehearsals": self.rehearsals,
            "cleanup": self.cleanup,
            "production_isolation": self.production_isolation,
            "commands": self.commands,
        }
        # Redact recursively by serializing once and parsing back.
        safe_json = self.redact(json.dumps(result, indent=2, sort_keys=True))
        JSON_REPORT.write_text(safe_json + "\n", encoding="utf-8")

        lines = []
        lines.append("ComposeDrill local verification report")
        lines.append("Generated: {}".format(finished.isoformat()))
        lines.append("Host: {} {} ({})".format(platform.system(), platform.release(), platform.machine()))
        lines.append("Python: {}".format(platform.python_version()))
        lines.append("")
        width = max(len(k) for k in self.matrix)
        for key, value in self.matrix.items():
            lines.append("{:<{}}  {}".format(key, width, value))
        if self.prerequisites:
            lines.append("")
            lines.append("PREREQUISITES")
            for item in self.prerequisites:
                lines.append("- " + item)
        if self.notes:
            lines.append("")
            lines.append("NOTES")
            for item in self.notes:
                lines.append("- " + item)
        lines.append("")
        lines.append("COMMAND EXIT CODES")
        for cmd in self.commands:
            lines.append("- {} => {}".format(cmd["display_command"], cmd["exit_code"]))
        lines.append("")
        lines.append("Full redacted command output: artifacts/full_validation.log")
        lines.append("Machine-readable report: artifacts/local_verification_report.json")
        TEXT_REPORT.write_text(self.redact("\n".join(lines)) + "\n", encoding="utf-8")


def shell_quote(value):
    if re.fullmatch(r"[A-Za-z0-9_./:=+@%-]+", value):
        return value
    return "'" + value.replace("'", "'\\''") + "'"


def parse_go_version(output):
    match = re.search(r"\bgo(\d+)\.(\d+)(?:\.(\d+))?\b", output)
    if not match:
        return None
    return tuple(int(x or 0) for x in match.groups())


def required_go_version():
    path = ROOT / "go.mod"
    try:
        for line in path.read_text(encoding="utf-8").splitlines():
            line = line.strip()
            if line.startswith("go "):
                bits = line.split()[1].split(".")
                return tuple(int(x) for x in bits[:2])
    except Exception:
        pass
    return (1, 23)


def snapshot_tree(root):
    root = Path(root)
    snap = {}
    if not root.exists():
        return snap
    paths = [root] + sorted(root.rglob("*"), key=lambda p: str(p.relative_to(root)))
    for path in paths:
        rel = "." if path == root else str(path.relative_to(root))
        try:
            st = path.lstat()
        except OSError as exc:
            snap[rel] = {"type": "error", "error": str(exc)}
            continue
        mode = stat.S_IMODE(st.st_mode)
        if path.is_symlink():
            snap[rel] = {"type": "symlink", "mode": mode, "target": os.readlink(str(path))}
        elif path.is_dir():
            snap[rel] = {"type": "dir", "mode": mode}
        elif path.is_file():
            h = hashlib.sha256()
            with path.open("rb") as fp:
                while True:
                    chunk = fp.read(1024 * 1024)
                    if not chunk:
                        break
                    h.update(chunk)
            snap[rel] = {
                "type": "file",
                "mode": mode,
                "size": st.st_size,
                "sha256": h.hexdigest(),
            }
        else:
            snap[rel] = {"type": "other", "mode": mode}
    return snap


def snapshot_diff(before, after):
    changed = []
    for key in sorted(set(before) | set(after)):
        if key not in before:
            changed.append({"path": key, "change": "ADDED", "after": after[key]})
        elif key not in after:
            changed.append({"path": key, "change": "REMOVED", "before": before[key]})
        elif before[key] != after[key]:
            changed.append({"path": key, "change": "MODIFIED", "before": before[key], "after": after[key]})
    return changed


def load_evidence(work_root):
    evidence_dir = Path(work_root) / ".composedrill" / "evidence"
    candidates = sorted(evidence_dir.glob("*.json"), key=lambda p: p.stat().st_mtime_ns if p.exists() else 0)
    if not candidates:
        return None, None
    path = candidates[-1]
    try:
        return path, json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        return path, {"_parse_error": str(exc)}


def summarize_evidence(data):
    if not isinstance(data, dict):
        return None
    return {
        "run_id": data.get("run_id"),
        "status": data.get("status"),
        "cleanup_status": data.get("cleanup_status"),
        "recovery_project_name": data.get("recovery_project_name"),
        "services": [
            {
                "service": s.get("service"),
                "configuration": s.get("configuration"),
                "data_restore": s.get("data_restore"),
                "startup": s.get("startup"),
                "health": s.get("health"),
                "reason": s.get("reason"),
                "checks": s.get("checks", []),
            }
            for s in data.get("services", [])
            if isinstance(s, dict)
        ],
        "findings": data.get("findings", []),
    }


def success_evidence_ok(data):
    if not isinstance(data, dict) or data.get("status") != "PASS":
        return False, "evidence status was not PASS"
    if data.get("cleanup_status") != "PASS":
        return False, "cleanup_status was not PASS"
    services = data.get("services") or []
    if not services:
        return False, "evidence contained no services"
    for service in services:
        for field in ("configuration", "data_restore", "startup", "health"):
            if service.get(field) != "PASS":
                return False, "service {} {} was {}".format(service.get("service"), field, service.get(field))
    return True, "overall evidence and all service phases are PASS"


def broken_fixture_contract():
    prod_required = ROOT / "examples" / "broken" / "data" / "required.txt"
    bundle = ROOT / "examples" / "broken" / "recovery-bundle"
    bundle_required = bundle / "bind-data" / "required.txt"
    compose_path = bundle / "compose.yaml"
    manifest_path = bundle / "composedrill-backup.json"
    details = {
        "production_required_file_exists": prod_required.is_file(),
        "backup_required_file_absent": not bundle_required.exists(),
        "healthcheck_requires_file": False,
        "manifest_maps_bind_data": False,
    }
    try:
        compose = compose_path.read_text(encoding="utf-8")
        details["healthcheck_requires_file"] = "test -f /data/required.txt" in compose
    except OSError:
        pass
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        for entry in manifest.get("entries", []):
            if (
                entry.get("kind") == "bind"
                and entry.get("service") == "app"
                and entry.get("target") == "/data"
                and entry.get("artifact") == "bind-data"
            ):
                details["manifest_maps_bind_data"] = True
                break
    except Exception:
        pass
    ok = all(details.values())
    return ok, details


def classify_broken(exit_code, data, contract_ok):
    if exit_code == 0 or (isinstance(data, dict) and data.get("status") == "PASS"):
        return "UNEXPECTED PASS", "broken fixture unexpectedly passed"
    if not contract_ok:
        return "WRONG FAILURE", "broken fixture contract is not the intended missing-data health failure"
    if not isinstance(data, dict):
        return "WRONG FAILURE", "no recovery evidence was produced"
    if data.get("_parse_error"):
        return "WRONG FAILURE", "recovery evidence could not be parsed"
    if data.get("status") != "FAIL":
        return "WRONG FAILURE", "evidence status was {}, expected FAIL".format(data.get("status"))
    if data.get("cleanup_status") != "PASS":
        return "WRONG FAILURE", "recovery failed but cleanup did not pass"
    services = [s for s in data.get("services", []) if isinstance(s, dict) and s.get("service") == "app"]
    if len(services) != 1:
        return "WRONG FAILURE", "expected exactly one app service in broken-fixture evidence"
    service = services[0]
    if service.get("configuration") != "PASS":
        return "WRONG FAILURE", "configuration failed before the intended application-health check"
    if service.get("data_restore") != "PASS":
        return "WRONG FAILURE", "data restoration itself failed instead of restoring the intentionally incomplete backup"
    if service.get("startup") != "PASS":
        return "WRONG FAILURE", "container startup failed before the intended application-health failure"
    if service.get("health") != "FAIL":
        return "WRONG FAILURE", "application health did not fail as intended"
    reason = str(service.get("reason") or "").lower()
    health_reason = ("unhealthy" in reason) or ("required" in reason) or ("health" in reason)
    if not health_reason:
        return "WRONG FAILURE", "health failed but evidence did not identify an unhealthy/application-health condition"
    return (
        "EXPECTED FAIL",
        "incomplete bind backup restored successfully, then app health failed because the fixture healthcheck requires /data/required.txt",
    )


def parse_integration_output(output, exit_code):
    # Parse RUN/FAIL/SKIP rather than relying only on PASS lines. Validation
    # output is redacted before it is stored, and a locally configured secret
    # value can legitimately replace a test name on a PASS line. RUN lines are
    # sufficient to prove execution; a test counts as passed only when it ran
    # and is absent from the explicit FAIL/SKIP sets. The overall go test exit
    # code must still be zero, so crashes/package failures cannot become passes.
    ran = set(re.findall(r"=== RUN\s+(Test[A-Za-z0-9_]+)", output))
    skipped = set(re.findall(r"--- SKIP:\s+(Test[A-Za-z0-9_]+)", output))
    failed = set(re.findall(r"--- FAIL:\s+(Test[A-Za-z0-9_]+)", output))
    passed = ran - skipped - failed
    missing = EXPECTED_INTEGRATION_TESTS - ran
    ok = (
        exit_code == 0
        and EXPECTED_INTEGRATION_TESTS.issubset(ran)
        and EXPECTED_INTEGRATION_TESTS.issubset(passed)
        and not skipped
        and not failed
    )
    return ok, {
        "expected": sorted(EXPECTED_INTEGRATION_TESTS),
        "ran": sorted(ran),
        "passed": sorted(passed),
        "skipped": sorted(skipped),
        "failed": sorted(failed),
        "missing": sorted(missing),
    }


def docker_resources(h, label):
    queries = [
        ("containers", ["docker", "ps", "-a", "--format", "{{.ID}}\t{{.Names}}\t{{.Label \"io.composedrill.managed\"}}\t{{.Label \"io.composedrill.run\"}}"]),
        ("networks", ["docker", "network", "ls", "--format", "{{.ID}}\t{{.Name}}\t{{.Label \"io.composedrill.managed\"}}\t{{.Label \"io.composedrill.run\"}}"]),
        ("volumes", ["docker", "volume", "ls", "--format", "{{.Name}}\t{{.Label \"io.composedrill.managed\"}}\t{{.Label \"io.composedrill.run\"}}"]),
    ]
    result = {}
    all_ok = True
    for kind, cmd in queries:
        rec = h.run(cmd, "{}: list Docker {}".format(label, kind), timeout=60)
        if rec["exit_code"] != 0:
            result[kind] = {"query_ok": False, "resources": [], "error": rec["output"]}
            all_ok = False
            continue
        rows = []
        for line in rec["output"].splitlines():
            fields = line.split("\t")
            if not fields:
                continue
            # Containers/networks: id, name, managed, run. Volumes: name, managed, run.
            if kind == "volumes":
                name = fields[0] if len(fields) > 0 else ""
                managed = fields[1] if len(fields) > 1 else ""
                run_id = fields[2] if len(fields) > 2 else ""
                ident = name
            else:
                ident = fields[0] if len(fields) > 0 else ""
                name = fields[1] if len(fields) > 1 else ""
                managed = fields[2] if len(fields) > 2 else ""
                run_id = fields[3] if len(fields) > 3 else ""
            if managed == "true" or name.startswith("composedrill-") or "_composedrill-" in name:
                rows.append({"id": ident, "name": name, "managed": managed, "run_id": run_id})
        result[kind] = {"query_ok": True, "resources": rows}
    return all_ok, result


def main():
    h = Harness()
    temp_base = None
    release_ok = False
    before_examples = snapshot_tree(ROOT / "examples")
    try:
        h.log("ComposeDrill deterministic local release gate")
        h.log("Repository: {}".format(ROOT))
        h.log("Started: {}".format(h.started.isoformat()))
        h.log("Host: {} {} ({})".format(platform.system(), platform.release(), platform.machine()))
        h.log("Python: {} ({})".format(platform.python_version(), sys.executable))

        # Platform and Python detection.
        if platform.system() != "Darwin":
            h.prerequisite("This validation harness is intentionally scoped to macOS; run it on a Mac.")
        if sys.version_info.major != 3:
            h.prerequisite("Python 3 is required. Run the harness as: python3 dev.py")

        # Go toolchain.
        go_path = shutil.which("go")
        go_ok = False
        if not go_path:
            h.matrix["GO TOOLCHAIN"] = "FAIL"
            h.prerequisite("Install Go {}.{} or newer, ensure `go` is on PATH, then rerun `python3 dev.py`.".format(*required_go_version()))
        else:
            rec = h.run([go_path, "version"], "Detect Go", timeout=30)
            found = parse_go_version(rec["output"])
            required = required_go_version()
            go_ok = rec["exit_code"] == 0 and found is not None and found[:2] >= required
            h.matrix["GO TOOLCHAIN"] = "PASS" if go_ok else "FAIL"
            if not go_ok:
                h.prerequisite("Go {}.{} or newer is required by go.mod; update Go and rerun `python3 dev.py`.".format(*required))

        # Docker CLI, daemon, and Compose v2.
        docker_path = shutil.which("docker")
        docker_ok = False
        compose_ok = False
        if not docker_path:
            h.matrix["DOCKER ENGINE"] = "FAIL"
            h.matrix["DOCKER COMPOSE"] = "FAIL"
            arch = platform.machine().lower()
            flavor = "Apple silicon" if arch in ("arm64", "aarch64") else "Intel Mac" if arch in ("x86_64", "amd64") else arch
            h.prerequisite(
                "Docker CLI/engine not found. Install Docker Desktop for Mac ({}) or another Docker Engine-compatible macOS runtime that provides the `docker` CLI and Docker Compose v2; start it, verify `docker info` succeeds, then rerun. No system software is installed automatically.".format(flavor)
            )
        else:
            rec = h.run([docker_path, "--version"], "Detect Docker CLI", timeout=30)
            if rec["exit_code"] != 0:
                h.matrix["DOCKER ENGINE"] = "FAIL"
                h.prerequisite("The `docker` command exists but is not usable. Repair the Docker installation and rerun.")
            else:
                info = h.run([docker_path, "info", "--format", "{{.ServerVersion}}"], "Confirm Docker daemon", timeout=60)
                docker_ok = info["exit_code"] == 0 and bool(info["output"].strip())
                h.matrix["DOCKER ENGINE"] = "PASS" if docker_ok else "FAIL"
                if not docker_ok:
                    h.prerequisite(
                        "Docker is installed but the daemon is not reachable. Launch Docker Desktop (or your compatible Docker runtime), wait until it reports the engine is running, confirm `docker info` succeeds, then rerun."
                    )
                compose = h.run([docker_path, "compose", "version"], "Detect Docker Compose", timeout=30)
                compose_ok = compose["exit_code"] == 0 and "compose" in compose["output"].lower()
                h.matrix["DOCKER COMPOSE"] = "PASS" if compose_ok else "FAIL"
                if not compose_ok:
                    h.prerequisite("Docker Compose v2 is required (`docker compose version` must succeed). Install/enable the Compose v2 plugin and rerun.")

        prereq_ok = (
            platform.system() == "Darwin"
            and sys.version_info.major == 3
            and go_ok
            and docker_ok
            and compose_ok
        )

        # Record any pre-existing ComposeDrill resources. Never delete them automatically.
        pre_resources = None
        if docker_ok:
            _, pre_resources = docker_resources(h, "Preflight cleanup audit")
            h.cleanup["preexisting"] = pre_resources
            pre_count = sum(len(v.get("resources", [])) for v in pre_resources.values())
            if pre_count:
                h.note("Pre-existing ComposeDrill-named/managed Docker resources were found. The harness will not delete them automatically; CLEANUP cannot pass until the host starts and ends clean.")

        if prereq_ok:
            temp_base = Path(tempfile.mkdtemp(prefix="composedrill-local-validation-"))
            h.log("Temporary rehearsal root: {}".format(temp_base))

            # Requested Go release gate.
            unit = h.run([go_path, "test", "./..."], "Unit tests", timeout=900)
            h.matrix["UNIT TESTS"] = "PASS" if unit["exit_code"] == 0 else "FAIL"

            race = h.run([go_path, "test", "-race", "./..."], "Race tests", timeout=1200)
            h.matrix["RACE TESTS"] = "PASS" if race["exit_code"] == 0 else "FAIL"

            vet = h.run([go_path, "vet", "./..."], "Go vet", timeout=900)
            h.matrix["GO VET"] = "PASS" if vet["exit_code"] == 0 else "FAIL"

            if BINARY.exists():
                BINARY.unlink()
            build = h.run(
                [go_path, "build", "-trimpath", "-o", str(BINARY), "./cmd/composedrill"],
                "Build ComposeDrill",
                timeout=900,
            )
            build_ok = build["exit_code"] == 0 and BINARY.is_file()
            h.matrix["BUILD"] = "PASS" if build_ok else "FAIL"

            integration = h.run(
                [go_path, "test", "-tags=integration", "-count=1", "-v", "./internal/integration"],
                "Docker integration suite",
                timeout=1800,
            )
            integration_ok, integration_detail = parse_integration_output(integration["output"], integration["exit_code"])
            h.matrix["INTEGRATION SUITE"] = "PASS" if integration_ok else "FAIL"
            h.rehearsals["integration_suite"] = integration_detail
            if integration_detail["skipped"]:
                h.note("Integration tests were skipped: {}. A zero `go test` exit code is not accepted as a Docker integration pass.".format(", ".join(integration_detail["skipped"])))

            # Manual fixture rehearsals exercise the built CLI and inspect evidence.
            if build_ok:
                for fixture in SUCCESS_FIXTURES:
                    work_root = temp_base / fixture
                    work_root.mkdir(parents=True, exist_ok=True)
                    backup = ROOT / "examples" / fixture / "recovery-bundle"
                    rec = h.run(
                        [str(BINARY), "rehearse", "--backup", str(backup), str(work_root)],
                        "{} recovery rehearsal".format(fixture),
                        timeout=600,
                    )
                    evidence_path, evidence = load_evidence(work_root)
                    ok, why = success_evidence_ok(evidence)
                    ok = ok and rec["exit_code"] == 0
                    key = {
                        "stateless": "STATELESS RESTORE",
                        "postgres": "POSTGRES RESTORE",
                        "sqlite": "SQLITE RESTORE",
                        "bind": "BIND RESTORE",
                    }[fixture]
                    h.matrix[key] = "PASS" if ok else "FAIL"
                    h.rehearsals[fixture] = {
                        "exit_code": rec["exit_code"],
                        "evidence_path": str(evidence_path) if evidence_path else None,
                        "evidence": summarize_evidence(evidence),
                        "classification": "PASS" if ok else "FAIL",
                        "reason": why if rec["exit_code"] == 0 else "CLI exit code was {}. {}".format(rec["exit_code"], why),
                    }
                    h.log("{} evidence classification: {} — {}".format(fixture, h.matrix[key], h.rehearsals[fixture]["reason"]))

                contract_ok, contract_detail = broken_fixture_contract()
                broken_root = temp_base / "broken"
                broken_root.mkdir(parents=True, exist_ok=True)
                broken = h.run(
                    [str(BINARY), "rehearse", "--backup", str(ROOT / "examples" / "broken" / "recovery-bundle"), str(broken_root)],
                    "broken recovery rehearsal (expected application-health failure)",
                    timeout=600,
                )
                evidence_path, evidence = load_evidence(broken_root)
                classification, why = classify_broken(broken["exit_code"], evidence, contract_ok)
                h.matrix["BROKEN FIXTURE"] = classification
                h.rehearsals["broken"] = {
                    "exit_code": broken["exit_code"],
                    "fixture_contract": contract_detail,
                    "evidence_path": str(evidence_path) if evidence_path else None,
                    "evidence": summarize_evidence(evidence),
                    "classification": classification,
                    "reason": why,
                }
                h.log("broken fixture classification: {} — {}".format(classification, why))
            else:
                for key in ("STATELESS RESTORE", "POSTGRES RESTORE", "SQLITE RESTORE", "BIND RESTORE", "BROKEN FIXTURE"):
                    h.matrix[key] = "NOT RUN"
                h.note("Manual recovery rehearsals were not run because the ComposeDrill build failed.")

            # Postflight resource audit. A clean start and clean end are both required.
            query_ok, post_resources = docker_resources(h, "Postflight cleanup audit")
            h.cleanup["postflight"] = post_resources
            post_count = sum(len(v.get("resources", [])) for v in post_resources.values())
            pre_count = sum(len(v.get("resources", [])) for v in (pre_resources or {}).values())
            cleanup_ok = query_ok and pre_count == 0 and post_count == 0
            h.cleanup["preexisting_count"] = pre_count
            h.cleanup["postflight_count"] = post_count
            h.cleanup["result"] = "PASS" if cleanup_ok else "FAIL"
            h.matrix["CLEANUP"] = "PASS" if cleanup_ok else "FAIL"
            if post_count:
                h.note("Postflight found {} ComposeDrill-named/managed Docker resource(s). No automatic deletion was attempted; inspect the report before cleanup.".format(post_count))
        else:
            h.note("Release commands were not run because one or more required local prerequisites failed.")

        # Production fixture immutability is checked even when prerequisites fail.
        after_examples = snapshot_tree(ROOT / "examples")
        diffs = snapshot_diff(before_examples, after_examples)
        isolation_ok = len(diffs) == 0
        h.production_isolation = {
            "result": "PASS" if isolation_ok else "FAIL",
            "changed_paths": diffs,
            "scope": "entire examples/ tree, including file contents, modes, paths, and symlink targets",
        }
        h.matrix["PRODUCTION ISOLATION"] = "PASS" if isolation_ok else "FAIL"
        if diffs:
            h.note("Production fixture tree changed during validation; see production_isolation.changed_paths in the JSON report.")

        required_pass = [
            "GO TOOLCHAIN",
            "DOCKER ENGINE",
            "DOCKER COMPOSE",
            "UNIT TESTS",
            "RACE TESTS",
            "GO VET",
            "BUILD",
            "INTEGRATION SUITE",
            "STATELESS RESTORE",
            "POSTGRES RESTORE",
            "SQLITE RESTORE",
            "BIND RESTORE",
            "CLEANUP",
            "PRODUCTION ISOLATION",
        ]
        release_ok = (
            platform.system() == "Darwin"
            and sys.version_info.major == 3
            and all(h.matrix[k] == "PASS" for k in required_pass)
            and h.matrix["BROKEN FIXTURE"] == "EXPECTED FAIL"
        )
        h.matrix["RELEASE GATE"] = "PASS" if release_ok else "FAIL"

    except KeyboardInterrupt:
        h.note("Validation interrupted by user.")
        h.matrix["RELEASE GATE"] = "FAIL"
    except Exception as exc:
        h.note("Harness error: {}: {}".format(type(exc).__name__, exc))
        h.matrix["RELEASE GATE"] = "FAIL"
    finally:
        if temp_base is not None:
            try:
                shutil.rmtree(str(temp_base))
            except OSError as exc:
                h.note("Could not remove temporary validation directory {}: {}".format(temp_base, exc))
                h.matrix["CLEANUP"] = "FAIL"
                h.matrix["RELEASE GATE"] = "FAIL"
                release_ok = False
        h.write_reports()
        h.log("\n=== FINAL MATRIX ===")
        width = max(len(k) for k in h.matrix)
        for key, value in h.matrix.items():
            h.log("{:<{}}  {}".format(key, width, value))
        h.log("\nReports:")
        h.log("  {}".format(TEXT_REPORT.relative_to(ROOT)))
        h.log("  {}".format(JSON_REPORT.relative_to(ROOT)))
        h.log("  {}".format(FULL_LOG.relative_to(ROOT)))
        h.close()

    return 0 if release_ok else 1


if __name__ == "__main__":
    sys.exit(main())
