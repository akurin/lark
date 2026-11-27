# LARK A2 CLI

Lightweight Python 3 command-line access to the Hollyland LARK A2 USB-C receiver using hidapi. No daemon, audio capture, HollyAudio dependency, or sudo. Intended for Apple Silicon macOS and receiver USB ID `3547:0411`.

## Install

On the **Mac host**, from this directory:

```sh
./scripts/install.sh
lark doctor
lark battery
```

The idempotent installer creates `.venv`, installs `hidapi` if needed, installs the CLI entry point, and links `~/.local/bin/lark` to the virtual environment. It can reuse an existing virtual environment with `hidapi` already installed, without downloading a build backend. It reports if `~/.local/bin` is absent from `PATH` and never edits shell startup files. Run `./scripts/uninstall.sh` to remove this project's link and virtual environment.

## Commands

| Command | Result | Evidence |
| --- | --- | --- |
| `lark battery`, `lark status` | Batteries, connection, and additional state | Battery and connection hardware captured; other status fields from upstream |
| `lark battery --json` | Scriptable JSON object | Hardware captured battery format |
| `lark battery --watch --interval 5` | Poll until Ctrl+C; JSON mode emits one object per line | Same status request |
| `lark devices`, `lark doctor` | Enumerate interfaces and diagnose access | Local HID API |
| `lark raw` | Hex status payload; read-only | Hardware captured status command |
| `lark info`, `lark firmware` | Receiver identity and firmware | Firmware command from upstream source |
| `lark nc status`, `lark mute status`, `lark charging status` | Read-only states | Field positions from upstream source |
| `lark nc on`, `lark nc off`, `lark nc level 2` | Set noise cancellation and check reported state | Write packets tested by upstream on its receiver; **not tested on this receiver** |

`nc level` accepts 1, 2, or 3. The switch is a toggle in the protocol, so the CLI first reads current state and confirms changes with another read. Avoid simultaneous controllers. Firmware **updates** and other undocumented writes are not implemented. `raw` sends only the known status request; it does not accept arbitrary write packets.

JSON example:

```json
{"receiver_connected":true,"microphones":[{"id":1,"connected":true,"battery":76},{"id":2,"connected":false,"battery":null}]}
```

The JSON command emits data on stdout and errors on stderr. Exit codes: `0` success, `2` receiver absent or invalid arguments, `3` HID permission denied, `4` other HID I/O or unconfirmed control, `5` invalid response, `6` timeout, `130` interrupted. A connected microphone with an invalid battery byte reports `null` / `Unknown`.

## Troubleshooting

If `lark doctor` sees the receiver but cannot read it, grant **Input Monitoring** to the terminal app in **System Settings → Privacy & Security**, then restart the terminal. The receiver also exposes a keyboard HID interface. Close HollyAudio and other receiver controllers if the device is busy. Unplug and replug after a disconnect. Power on and pair a transmitter if both microphones show disconnected. No sudo is needed.

## Protocol and source attribution

Status framing, command `0x10`, and battery offsets were verified with the user's physical receiver. The captured 16-byte payload is `01 00 4c 00 00 00 00 05 00 00 00 00 00 00 04 00` and decodes to Mic 1 at 76%, Mic 2 disconnected. Firmware command `0x41`, noise byte 6, mute/charging bits in byte 15, and noise commands `0x19` and `0x06` were derived from [LARK Flow Bridge protocol notes](https://github.com/geekyshubham/lark-flow-bridge/blob/main/docs/HOW_IT_WORKS.md) and [Swift implementation](https://github.com/geekyshubham/lark-flow-bridge/blob/main/LARKFlowBridge.swift). The captured `0x04` is in byte 14, so it does **not** confirm the upstream charging bit at byte 15. That project is MIT licensed. This package uses its documented packet facts and an independent Python implementation; no upstream source was copied.

Run tests with `python3 -m unittest discover -s tests -v` after installing, or `.venv/bin/python -m unittest discover -s tests -v`.
