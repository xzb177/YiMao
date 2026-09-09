#!/usr/bin/env python3
"""Exercise the lifecycle script's real PID 1 check without Docker or delays."""

import json
import os
from pathlib import Path
import shlex
import signal
import subprocess
import sys
import tempfile
import time
import unittest


SCRIPT = Path(__file__).with_name("deployment_lifecycle.sh")
START = "# The test image overrides only Docker HEALTHCHECK. HTTP probing is replaced below.\n"
END = "# TERM during archive creation must abort and restore the original service.\n"
PASSED = "permission-check-passed"


def status(uid, gid):
    return f"Name:\tyimao\nUid:\t{uid}\t{uid}\t{uid}\t{uid}\nGid:\t{gid}\t{gid}\t{gid}\t{gid}\n"


def fixture(command, args):
    """Only simulate the Docker boundary; execute the actual status parser."""
    root = Path(os.environ["PID1_FIXTURE"])
    events_file = root / "events.json"
    events = json.loads(events_file.read_text())
    read_index = sum(event[0:2] == ["docker", "exec"] for event in events)
    events.append([command, *args])
    events_file.write_text(json.dumps(events))
    if command == "sleep":
        assert args == ["1"], f"unexpected sleep: {args!r}"
        return 0
    if args == ["inspect", "-f", "{{.State.Running}}", "pid1-fixture"]:
        if os.environ.get("PID1_HANG_RUNNING") == "1":
            signal.signal(signal.SIGTERM, signal.SIG_IGN)
            while True:
                time.sleep(1)
        print("true")
        return 0
    if args == ["inspect", "-f", "{{.State.Health.Status}}", "pid1-fixture"]:
        print("healthy")  # A healthy fixture is not proof of privilege drop.
        return 0
    if args[:2] != ["exec", "pid1-fixture"]:
        raise AssertionError(f"unexpected Docker command: {args!r}")
    parser = args[2:]
    if not parser or parser[0] not in ("awk", "cat") or parser.count("/proc/1/status") != 1:
        raise AssertionError(f"expected one PID 1 status read: {parser!r}")
    samples = json.loads((root / "samples.json").read_text())
    sample = samples[min(read_index, len(samples) - 1)]
    if sample.get("hang_ignore_term"):
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        while True:
            time.sleep(1)
    snapshot = root / "status"
    snapshot.write_text(sample["status"])
    parser = [str(snapshot) if arg == "/proc/1/status" else arg for arg in parser]
    result = subprocess.run(parser, check=False)
    return sample.get("exit", result.returncode)


class PID1PermissionTests(unittest.TestCase):
    def run_check(self, samples, *, hang_running=False):
        source = SCRIPT.read_text()
        self.assertEqual(source.count(START), 1)
        self.assertEqual(source.count(END), 1)
        fragment = source.split(START, 1)[1].split(END, 1)[0]
        self.assertIn("/proc/1/status", fragment)
        with tempfile.TemporaryDirectory(
            prefix="pid1-regression-", dir=os.environ.get("YIMAO_TEST_TMP_ROOT")
        ) as directory:
            root = Path(directory)
            (root / "samples.json").write_text(json.dumps(samples))
            (root / "events.json").write_text("[]")
            for command in ("docker", "sleep"):
                wrapper = root / command
                wrapper.write_text(
                    "#!/bin/sh\nexec " + shlex.join(
                        [sys.executable, str(Path(__file__).resolve()), "--fixture", command]
                    ) + ' "$@"\n'
                )
                wrapper.chmod(0o755)
            environment = dict(os.environ, PID1_FIXTURE=str(root), PATH=f"{root}:{os.environ['PATH']}")
            if hang_running:
                environment["PID1_HANG_RUNNING"] = "1"
            started = time.monotonic()
            result = subprocess.run(
                ["sh", "-eu", "-c", f"NAME=pid1-fixture\n{fragment}\nprintf '%s\\n' {PASSED}\n"],
                env=environment, capture_output=True, text=True, timeout=10, check=False,
            )
            self.last_elapsed = time.monotonic() - started
            events = json.loads((root / "events.json").read_text())
            self.assertNotIn("Traceback", result.stderr, "fixture failed: " + result.stderr)
        return result, events

    def test_waits_for_delayed_privilege_drop(self):
        result, events = self.run_check([
            {"status": status(0, 0)},
            {"status": status(0, 0)},
            {"status": status(10001, 10001)},
        ])
        self.assertEqual(result.returncode, 0, result.stderr + json.dumps(events))
        self.assertIn(PASSED, result.stdout)
        self.assertEqual(sum(event[0:2] == ["docker", "exec"] for event in events), 3)
        self.assertEqual(sum(event[0] == "sleep" for event in events), 2)

    def assert_denied(self, result):
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertNotIn(PASSED, result.stdout)
        self.assertIn("PID 1", result.stderr)

    def assert_reads_and_sleeps(self, events, reads, sleeps):
        self.assertEqual(sum(event[0:2] == ["docker", "exec"] for event in events), reads)
        self.assertEqual(sum(event[0] == "sleep" for event in events), sleeps)

    def test_already_dropped_uses_one_status_snapshot(self):
        result, events = self.run_check([{"status": status(10001, 10001)}])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(PASSED, result.stdout)
        self.assert_reads_and_sleeps(events, 1, 0)

    def test_accepts_drop_on_last_attempt(self):
        result, events = self.run_check(
            [{"status": status(0, 0)}] * 29 + [{"status": status(10001, 10001)}]
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(PASSED, result.stdout)
        self.assert_reads_and_sleeps(events, 30, 29)

    def test_root_times_out_despite_healthy_container(self):
        result, events = self.run_check([{"status": status(0, 0)}])
        self.assert_denied(result)
        self.assertIn("0:0", result.stderr)
        self.assertIn("timed out", result.stderr)
        self.assert_reads_and_sleeps(events, 30, 29)

    def test_wrong_gid_times_out(self):
        result, events = self.run_check([{"status": status(10001, 0)}])
        self.assert_denied(result)
        self.assertIn("10001:0", result.stderr)
        self.assert_reads_and_sleeps(events, 30, 29)

    def test_wrong_uid_times_out(self):
        result, events = self.run_check([{"status": status(0, 10001)}])
        self.assert_denied(result)
        self.assertIn("0:10001", result.stderr)
        self.assert_reads_and_sleeps(events, 30, 29)

    def test_does_not_combine_ids_from_different_snapshots(self):
        result, events = self.run_check([
            {"status": status(10001, 0)}, {"status": status(0, 10001)},
        ] * 15)
        self.assert_denied(result)
        self.assert_reads_and_sleeps(events, 30, 29)

    def test_read_failure_with_allowed_output_aborts(self):
        result, events = self.run_check([
            {"status": status(10001, 10001), "exit": 125},
            {"status": status(10001, 10001)},
        ])
        self.assert_denied(result)
        self.assert_reads_and_sleeps(events, 1, 0)

    def test_read_failure_after_root_aborts(self):
        result, events = self.run_check([
            {"status": status(0, 0)},
            {"status": "", "exit": 125},
            {"status": status(10001, 10001)},
        ])
        self.assert_denied(result)
        self.assert_reads_and_sleeps(events, 2, 1)

    def test_running_inspect_hang_is_forcibly_killed_and_aborts(self):
        result, events = self.run_check(
            [{"status": status(10001, 10001)}], hang_running=True
        )
        self.assert_denied(result)
        self.assertIn("cannot inspect container PID 1 startup", result.stderr)
        self.assertLess(self.last_elapsed, 5)
        self.assertEqual(sum(event[0:2] == ["docker", "exec"] for event in events), 0)

    def test_pid1_read_hang_is_forcibly_killed_and_aborts(self):
        result, events = self.run_check([
            {"status": status(10001, 10001), "hang_ignore_term": True},
        ])
        self.assert_denied(result)
        self.assertIn("cannot read valid container PID 1 UID:GID", result.stderr)
        self.assertLess(self.last_elapsed, 5)
        self.assert_reads_and_sleeps(events, 1, 0)

    def test_invalid_snapshot_aborts_instead_of_retrying(self):
        good = status(10001, 10001)
        invalid = {
            "empty": "",
            "missing_uid": "Gid:\t10001\t10001\t10001\t10001\n",
            "missing_gid": "Uid:\t10001\t10001\t10001\t10001\n",
            "empty_uid": good.replace("Uid:\t10001\t10001\t10001\t10001", "Uid:"),
            "nonnumeric": good.replace("Uid:\t10001", "Uid:\tinvalid"),
            "duplicate_uid": good + "Uid:\t10001\t10001\t10001\t10001\n",
            "duplicate_gid": good + "Gid:\t10001\t10001\t10001\t10001\n",
            "short_row": good.replace("Uid:\t10001\t10001\t10001\t10001", "Uid:\t10001"),
            "extra_field": good.replace("Uid:\t10001", "Uid:\t10001\textra"),
            "retained_root_uid": good.replace("Uid:\t10001\t10001", "Uid:\t10001\t0"),
            "retained_root_gid": good.replace("Gid:\t10001\t10001", "Gid:\t10001\t0"),
        }
        for name, snapshot in invalid.items():
            with self.subTest(snapshot=name):
                result, events = self.run_check([
                    {"status": snapshot}, {"status": good},
                ])
                self.assert_denied(result)
                self.assert_reads_and_sleeps(events, 1, 0)



if __name__ == "__main__":
    if sys.argv[1:2] == ["--fixture"]:
        sys.exit(fixture(sys.argv[2], sys.argv[3:]))
    unittest.main(verbosity=2)
