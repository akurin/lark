"""Packet framing and fields observed on the A2 USB-C receiver.

Status batteries are hardware captured; additional fields follow LARK Flow Bridge.
"""
from .models import Microphone, Status

REPORT_ID = 0x55
REPORT_SIZE = 64
STATUS_COMMAND = 0x10
FIRMWARE_COMMAND = 0x41


class ProtocolError(ValueError):
    pass


def request(command: int, payload: bytes = b"") -> bytes:
    if len(payload) > REPORT_SIZE - 7:
        raise ValueError("payload too long")
    return (bytes((REPORT_ID, 0xAA, 0xDD, command, 0x40, 0, len(payload))) + payload).ljust(REPORT_SIZE, b"\0")


def response(data: bytes, command: int) -> bytes:
    if data[:1] == bytes((REPORT_ID,)):
        data = data[1:]
    if len(data) < 6:
        raise ProtocolError("truncated response header")
    if data[:4] != bytes((0xBB, 0xDD, command, 0x80)):
        raise ProtocolError("unexpected response header, command, or destination")
    length = int.from_bytes(data[4:6], "big")
    if len(data) < 6 + length:
        raise ProtocolError(f"truncated response payload: expected {length} bytes")
    return data[6:6 + length]


def decode_status(payload: bytes) -> Status:
    if len(payload) != 16:
        raise ProtocolError(f"status payload must be 16 bytes, got {len(payload)}")
    microphones = []
    for i in range(2):
        if payload[i] not in (0, 1):
            raise ProtocolError(f"invalid Mic {i + 1} connection flag")
        connected = payload[i] == 1
        battery = payload[i + 2] if connected and payload[i + 2] <= 100 else None
        microphones.append(Microphone(
            i + 1, connected, battery,
            bool(payload[15] & (1 << i)) if connected else None,
            bool(payload[15] & (4 << i)) if connected else None,
        ))
    noise = payload[6]
    return Status(tuple(microphones), noise if noise <= 3 else None)


def decode_firmware(payload: bytes) -> str:
    if len(payload) < 6:
        raise ProtocolError("firmware payload must contain at least 6 bytes")
    return f"{payload[0]}.{payload[1]}.{payload[2]}.{payload[3]}{payload[4]}{payload[5]}"
