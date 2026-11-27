import argparse
import json
import os
import sys
import time

from . import __version__
from . import helper
from .device import (AccessError, BusyError, Disconnected, LarkError, NotFound,
                     ReadTimeout, Receiver, devices)
from .protocol import ProtocolError, STATUS_COMMAND

EXIT_NOT_FOUND = 2
EXIT_ACCESS = 3
EXIT_IO = 4
EXIT_PROTOCOL = 5
EXIT_TIMEOUT = 6


def parser():
    p = argparse.ArgumentParser(prog="lark", description="Hollyland LARK A2 USB HID toolkit")
    p.add_argument("--version", action="version", version=__version__)
    sub = p.add_subparsers(dest="command", required=True)
    sub.add_parser("status", help="show transmitter batteries and source-derived state")
    battery = sub.add_parser("battery", help="show transmitter battery levels")
    battery.add_argument("--json", action="store_true", help="emit one JSON object per sample")
    battery.add_argument("--watch", action="store_true", help="poll until Ctrl+C")
    battery.add_argument("--interval", type=float, default=5, help="watch interval in seconds (default: 5)")
    sub.add_parser("info", help="show receiver identity and firmware")
    sub.add_parser("firmware", help="read receiver firmware")
    sub.add_parser("devices", help="list HID interfaces, deduplicated by path")
    sub.add_parser("doctor", help="diagnose receiver access")
    sub.add_parser("raw", help="print one raw status payload (read-only)")
    nc = sub.add_parser("nc", help="noise cancellation (source-derived; writes require confirmation)")
    ncsub = nc.add_subparsers(dest="action", required=True)
    for action in ("status", "on", "off"):
        ncsub.add_parser(action)
    level = ncsub.add_parser("level")
    level.add_argument("value", type=int, choices=(1, 2, 3))
    mute = sub.add_parser("mute", help="read mute status")
    mute.add_subparsers(dest="action", required=True).add_parser("status")
    charging = sub.add_parser("charging", help="read charging status")
    charging.add_subparsers(dest="action", required=True).add_parser("status")
    return p


def _battery(status):
    for m in status.microphones:
        value = "Disconnected" if not m.connected else (f"{m.battery}%" if m.battery is not None else "Unknown")
        print(f"Mic {m.id}: {value}")


def _state(status, field):
    for m in status.microphones:
        value = "Disconnected" if not m.connected else ("Yes" if getattr(m, field) else "No")
        print(f"Mic {m.id}: {value}")


def _noise(status):
    return "Unknown" if status.noise_level is None else ("Off" if status.noise_level == 0 else f"Level {status.noise_level}")


def _confirm(receiver, target):
    for _ in range(4):
        time.sleep(0.1)
        if receiver.status().noise_level == target:
            return
    raise LarkError("noise setting was not confirmed by the receiver")


def _set_noise(receiver, action, level=None):
    current = receiver.status()
    if not any(m.connected for m in current.microphones):
        raise LarkError("power on a transmitter before changing noise cancellation")
    if current.noise_level is None:
        raise ProtocolError("unknown noise state; refusing to change it")
    if action == "off":
        if current.noise_level:
            receiver.send(0x19, b"\x02")
            _confirm(receiver, 0)
    elif action == "on":
        if current.noise_level == 0:
            receiver.send(0x19, b"\x02")
            # The switch may restore a previously used level; verify only that it is on.
            for _ in range(4):
                time.sleep(0.1)
                if (receiver.status().noise_level or 0) > 0:
                    break
            else:
                raise LarkError("noise cancellation did not turn on")
    elif action == "level":
        if current.noise_level == 0:
            receiver.send(0x19, b"\x02")
            time.sleep(0.08)
        receiver.send(0x06, bytes((level,)))
        _confirm(receiver, level)
    print(f"Noise cancellation: {_noise(receiver.status())}")


def _doctor():
    print("Hollyland LARK A2 diagnostic")
    entries = devices()
    if not entries:
        print("Receiver: absent. Plug in the LARK A2 USB-C receiver (3547:0411).")
        return EXIT_NOT_FOUND
    print(f"Receiver: {len(entries)} HID interface(s) found; usage pages: " +
          ", ".join(f"0x{e.get('usage_page', 0):04x}" for e in entries))
    if not any(e.get("usage_page") in (0xFF90, 0xFF82) for e in entries):
        print("No separate vendor collection is exposed; the receiver's control reports may use its Generic Desktop interface.")
    try:
        with Receiver() as receiver:
            status = receiver.status()
        print("HID status read: OK")
        _battery(status)
        if not any(m.connected for m in status.microphones):
            print("Power on and pair a transmitter to read its battery.")
        return 0
    except (LarkError, ProtocolError) as exc:
        print(f"HID status read: {exc}")
        if os.environ.get("LARK_IN_HELPER") == "1":
            print("In System Settings → Privacy & Security → Input Monitoring, enable Lark Helper or add ~/Applications/Lark Helper.app with the + button. Leave terminal apps disabled. Also close other receiver controllers and reconnect USB if needed.")
        else:
            print("Close other receiver controllers and reconnect USB if needed. If access still fails only in terminals without Input Monitoring, install the Lark helper app to isolate that permission.")
        return _exit_code(exc)


def _exit_code(exc):
    if isinstance(exc, NotFound):
        return EXIT_NOT_FOUND
    if isinstance(exc, AccessError):
        return EXIT_ACCESS
    if isinstance(exc, ReadTimeout):
        return EXIT_TIMEOUT
    if isinstance(exc, ProtocolError):
        return EXIT_PROTOCOL
    return EXIT_IO


def run(args):
    if args.command == "devices":
        entries = devices()
        if not entries:
            print("No LARK A2 HID interfaces found")
            return EXIT_NOT_FOUND
        for entry in entries:
            print(f"3547:0411 usage_page=0x{entry.get('usage_page', 0):04x} usage=0x{entry.get('usage', 0):04x} path={entry.get('path', b'')!r}")
        return 0
    if args.command == "doctor":
        return _doctor()
    if args.command == "battery" and args.interval <= 0:
        raise ValueError("--interval must be positive")
    with Receiver() as receiver:
        if args.command == "battery":
            while True:
                status = receiver.status()
                if args.json:
                    print(json.dumps(status.battery_json()))
                else:
                    _battery(status)
                sys.stdout.flush()
                if not args.watch:
                    return 0
                time.sleep(args.interval)
        if args.command == "status":
            status = receiver.status()
            _battery(status)
            print(f"Noise cancellation: {_noise(status)}")
            print("Muted:")
            _state(status, "muted")
            print("Charging:")
            _state(status, "charging")
        elif args.command == "raw":
            print(receiver.exchange(STATUS_COMMAND).hex(" "))
        elif args.command in ("info", "firmware"):
            if args.command == "info":
                print("Hollyland LARK A2 USB-C receiver (3547:0411)")
            print(f"Firmware: {receiver.firmware()}")
        elif args.command == "nc":
            if args.action == "status":
                print(f"Noise cancellation: {_noise(receiver.status())}")
            else:
                _set_noise(receiver, args.action, getattr(args, "value", None))
        elif args.command in ("mute", "charging"):
            _state(receiver.status(), "muted" if args.command == "mute" else "charging")
    return 0


def main(argv=None):
    args = parser().parse_args(argv)
    try:
        if args.command != "devices" and helper.available():
            forwarded = list(sys.argv[1:] if argv is None else argv)
            return helper.run(forwarded, watch=args.command == "battery" and args.watch)
        return run(args)
    except KeyboardInterrupt:
        print(file=sys.stderr)
        return 130
    except ValueError as exc:
        print(f"lark: {exc}", file=sys.stderr)
        return 2
    except (LarkError, ProtocolError) as exc:
        print(f"lark: {exc}", file=sys.stderr)
        return _exit_code(exc)


if __name__ == "__main__":
    sys.exit(main())
