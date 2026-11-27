import unittest
from unittest.mock import patch

from lark.device import Disconnected, ReadTimeout, Receiver, devices
from test_protocol import PACKET


class FakeDevice:
    def __init__(self, packet=PACKET):
        self.packet = packet
        self.closed = False

    def open_path(self, path):
        self.path = path

    def write(self, packet):
        self.last_write = packet
        return len(packet)

    def read(self, size, timeout):
        if isinstance(self.packet, Exception):
            raise self.packet
        return self.packet

    def close(self):
        self.closed = True


class FakeHid:
    def __init__(self, dev):
        self.dev = dev

    def enumerate(self, vid, pid):
        return [{"path": b"vendor", "usage_page": 0xFF90},
                {"path": b"vendor", "usage_page": 0xFF90},
                {"path": b"keyboard", "usage_page": 1}]

    def device(self):
        return self.dev


class DeviceTests(unittest.TestCase):
    def test_duplicate_interfaces(self):
        with patch("lark.device._hid", return_value=FakeHid(FakeDevice())):
            self.assertEqual(len(devices()), 2)

    def test_status_and_close(self):
        dev = FakeDevice()
        with patch("lark.device._hid", return_value=FakeHid(dev)):
            with Receiver() as receiver:
                self.assertEqual(receiver.status().microphones[0].battery, 76)
                self.assertEqual(dev.path, b"vendor")
        self.assertTrue(dev.closed)

    def test_timeout(self):
        with patch("lark.device._hid", return_value=FakeHid(FakeDevice(b""))):
            with Receiver() as receiver, self.assertRaises(ReadTimeout):
                receiver.status()

    def test_disconnect(self):
        with patch("lark.device._hid", return_value=FakeHid(FakeDevice(OSError("device disconnected")))):
            with Receiver() as receiver, self.assertRaises(Disconnected):
                receiver.status()

    def test_control_write_does_not_wait_for_ack(self):
        dev = FakeDevice(b"")
        with patch("lark.device._hid", return_value=FakeHid(dev)):
            with Receiver() as receiver:
                receiver.send(0x19, b"\x02")
        self.assertEqual(dev.last_write[:8], bytes.fromhex("55 aa dd 19 40 00 01 02"))


if __name__ == "__main__":
    unittest.main()
