import contextlib
import io
import json
from pathlib import Path
import plistlib
import tempfile
import unittest
from unittest.mock import patch

from lark import helper


class HelperTests(unittest.TestCase):
    def test_available_only_for_mac_app(self):
        with tempfile.TemporaryDirectory() as temp:
            app = Path(temp) / "Lark Helper.app"
            executable = app / "Contents" / "MacOS" / "LarkHelper"
            executable.parent.mkdir(parents=True)
            executable.touch()
            with patch.object(helper, "APP", app), patch.object(helper.sys, "platform", "darwin"), patch.dict("os.environ", {}, clear=True):
                self.assertTrue(helper.available())
                with patch.dict("os.environ", {"LARK_IN_HELPER": "1"}):
                    self.assertFalse(helper.available())
                with patch.dict("os.environ", {"LARK_DIRECT": "1"}):
                    self.assertFalse(helper.available())

    def test_result_and_streams(self):
        def launch(command, **_):
            if command[1] == "bootstrap":
                with Path(command[-1]).open("rb") as handle:
                    agent = plistlib.load(handle)
                root = Path(agent["ProgramArguments"][1])
                self.assertEqual(json.loads((root / "request.json").read_text()), {"argv": ["battery", "--json"]})
                (root / "ready").touch()
                (root / "stdout").write_text('{"receiver_connected":true}\n')
                (root / "stderr").write_text("diagnostic\n")
                (root / "result.json").write_text('{"exit_code": 5}')
            return type("Result", (), {"returncode": 0, "stderr": ""})()

        out, err = io.StringIO(), io.StringIO()
        with patch.object(helper.subprocess, "run", side_effect=launch), contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = helper.run(["battery", "--json"])
        self.assertEqual(code, 5)
        self.assertEqual(out.getvalue(), '{"receiver_connected":true}\n')
        self.assertEqual(err.getvalue(), "diagnostic\n")

    def test_watch_interrupt_signals_helper(self):
        root = None
        sleeps = 0

        def launch(command, **_):
            nonlocal root
            if command[1] == "bootstrap":
                with Path(command[-1]).open("rb") as handle:
                    agent = plistlib.load(handle)
                root = Path(agent["ProgramArguments"][1])
                (root / "ready").touch()
            return type("Result", (), {"returncode": 0, "stderr": ""})()

        def sleep(_):
            nonlocal sleeps
            sleeps += 1
            if sleeps == 1:
                raise KeyboardInterrupt
            self.assertTrue((root / "cancel").exists())
            (root / "result.json").write_text('{"exit_code": 15}')

        with patch.object(helper.subprocess, "run", side_effect=launch), patch.object(helper.time, "sleep", side_effect=sleep), contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(helper.run(["battery", "--watch"], watch=True), 130)


if __name__ == "__main__":
    unittest.main()
