import contextlib
import io
import json
import unittest
from unittest.mock import patch

from lark import cli
from lark.device import NotFound, ReadTimeout
from lark.protocol import decode_status
from test_protocol import PAYLOAD


class FakeReceiver:
    def __enter__(self):
        return self

    def __exit__(self, *_):
        pass

    def status(self):
        return decode_status(PAYLOAD)


class CliTests(unittest.TestCase):
    def call(self, args):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = cli.main(args)
        return code, out.getvalue(), err.getvalue()

    def test_battery(self):
        with patch("lark.cli.Receiver", FakeReceiver):
            code, out, err = self.call(["battery"])
        self.assertEqual((code, out, err), (0, "Mic 1: 76%\nMic 2: Disconnected\n", ""))

    def test_json(self):
        with patch("lark.cli.Receiver", FakeReceiver):
            code, out, err = self.call(["battery", "--json"])
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(out), {"receiver_connected": True, "microphones": [
            {"id": 1, "connected": True, "battery": 76},
            {"id": 2, "connected": False, "battery": None}]})
        self.assertEqual(err, "")

    def test_exit_codes(self):
        with patch("lark.cli.Receiver", side_effect=NotFound("absent")):
            self.assertEqual(self.call(["battery"])[0], cli.EXIT_NOT_FOUND)
        with patch("lark.cli.Receiver", side_effect=ReadTimeout("timeout")):
            self.assertEqual(self.call(["battery"])[0], cli.EXIT_TIMEOUT)
        self.assertEqual(self.call(["battery", "--interval", "0"])[0], 2)

    def test_read_only_state(self):
        with patch("lark.cli.Receiver", FakeReceiver):
            self.assertIn("Noise cancellation: Off", self.call(["nc", "status"])[1])
            self.assertIn("Mic 1: No", self.call(["charging", "status"])[1])


if __name__ == "__main__":
    unittest.main()
