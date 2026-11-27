"""Short-lived hidapi access. No daemon or persistent USB claim outside watch."""
import errno

from . import protocol

VID = 0x3547
PID = 0x0411
USAGE_PAGES = (0xFF90, 0xFF82)


class LarkError(Exception):
    pass


class NotFound(LarkError):
    pass


class AccessError(LarkError):
    pass


class BusyError(LarkError):
    pass


class Disconnected(LarkError):
    pass


class ReadTimeout(LarkError):
    pass


def _hid():
    try:
        import hid
        return hid
    except ImportError as exc:
        raise LarkError("hidapi is missing; run scripts/install.sh") from exc


def _classify(exc: Exception, *, connected: bool = False) -> LarkError:
    message = str(exc).lower()
    if "permission" in message or "access denied" in message or getattr(exc, "errno", None) in (errno.EACCES, errno.EPERM):
        return AccessError("HID access denied; macOS may require Input Monitoring for the responsible app. Use a dedicated Lark helper if you do not want to grant your terminal access")
    if "busy" in message or "exclusive" in message or getattr(exc, "errno", None) == errno.EBUSY:
        return BusyError("HID device busy; close HollyAudio or other receiver controllers")
    if connected:
        return Disconnected("receiver disconnected or HID I/O failed")
    return LarkError(f"could not open HID receiver: {exc}")


def devices() -> list[dict]:
    try:
        entries = _hid().enumerate(VID, PID)
    except Exception as exc:
        raise _classify(exc) from exc
    unique = {}
    for entry in entries:
        path = entry.get("path")
        if path is not None:
            unique[path] = entry
    return list(unique.values())


class Receiver:
    def __init__(self):
        self.dev = None

    def __enter__(self):
        entries = devices()
        if not entries:
            raise NotFound("LARK A2 receiver not found (USB 3547:0411); plug in the USB-C receiver")
        # Some macOS HID descriptors enumerate only a Generic Desktop collection,
        # even though that collection carries the receiver's vendor control reports.
        candidates = sorted(entries, key=lambda e: (
            e.get("usage_page") not in USAGE_PAGES,
            e.get("usage_page") != 0xFF90,
        ))
        errors = []
        for entry in candidates:
            dev = _hid().device()
            try:
                dev.open_path(entry["path"])
                self.dev = dev
                return self
            except Exception as exc:
                errors.append(_classify(exc))
                dev.close()
        # The user's hardware probe succeeded with hid.device().open(VID, PID).
        # On a single-interface macOS device this is unambiguous and may work
        # when hidapi's path-based open fails for that same collection.
        if len(entries) == 1:
            dev = _hid().device()
            try:
                dev.open(VID, PID)
                self.dev = dev
                return self
            except Exception as exc:
                errors.append(_classify(exc))
                dev.close()
        raise next((e for e in errors if isinstance(e, AccessError)), errors[0])

    def __exit__(self, *_):
        if self.dev is not None:
            self.dev.close()
            self.dev = None

    def exchange(self, command: int, payload: bytes = b"", timeout_ms: int = 1000) -> bytes:
        self.send(command, payload)
        try:
            data = bytes(self.dev.read(256, timeout_ms))
        except Exception as exc:
            raise _classify(exc, connected=True) from exc
        if not data:
            raise ReadTimeout("receiver did not answer within 1 second")
        return protocol.response(data, command)

    def send(self, command: int, payload: bytes = b"") -> None:
        try:
            written = self.dev.write(protocol.request(command, payload))
        except Exception as exc:
            raise _classify(exc, connected=True) from exc
        if written <= 0:
            raise Disconnected("receiver disconnected during write")

    def status(self):
        return protocol.decode_status(self.exchange(protocol.STATUS_COMMAND))

    def firmware(self):
        return protocol.decode_firmware(self.exchange(protocol.FIRMWARE_COMMAND))
