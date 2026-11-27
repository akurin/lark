import unittest

from lark.protocol import (ProtocolError, decode_firmware, decode_status,
                           request, response)

PAYLOAD = bytes.fromhex("01 00 4c 00 00 00 00 05 00 00 00 00 00 00 04 00")
PACKET = bytes.fromhex("55 bb dd 10 80 00 10") + PAYLOAD


class ProtocolTests(unittest.TestCase):
    def test_request(self):
        packet = request(0x10)
        self.assertEqual(len(packet), 64)
        self.assertEqual(packet[:7], bytes.fromhex("55 aa dd 10 40 00 00"))

    def test_captured_battery_and_optional_id(self):
        for packet in (PACKET, PACKET[1:]):
            status = decode_status(response(packet, 0x10))
            self.assertEqual(status.microphones[0].battery, 76)
            self.assertFalse(status.microphones[1].connected)
            self.assertIsNone(status.microphones[1].battery)

    def test_battery_limits(self):
        for percent in (0, 100):
            payload = bytearray(PAYLOAD)
            payload[2] = percent
            self.assertEqual(decode_status(payload).microphones[0].battery, percent)
        payload[2] = 101
        self.assertIsNone(decode_status(payload).microphones[0].battery)

    def test_mute_charging_and_noise(self):
        status = decode_status(PAYLOAD)
        self.assertFalse(status.microphones[0].muted)
        self.assertFalse(status.microphones[0].charging)
        self.assertIsNone(status.microphones[1].charging)
        self.assertEqual(status.noise_level, 0)
        charging = bytearray(PAYLOAD)
        charging[15] = 0x04
        self.assertTrue(decode_status(charging).microphones[0].charging)

    def test_malformed(self):
        for packet in (b"", PACKET[:5], PACKET[:12], b"\x55\xbb\xdd\x11\x80\x00\x10" + PAYLOAD):
            with self.assertRaises(ProtocolError):
                response(packet, 0x10)
        with self.assertRaises(ProtocolError):
            decode_status(PAYLOAD[:-1])
        with self.assertRaises(ProtocolError):
            decode_firmware(b"\x01")


if __name__ == "__main__":
    unittest.main()
