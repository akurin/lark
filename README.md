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

### Keep Input Monitoring off your terminal

Some A2 receivers enumerate on macOS as a single Generic Desktop HID interface. If macOS denies direct access from your terminal, build the optional short-lived helper app **on the Mac**:

```sh
./scripts/install-helper.sh
lark doctor
```

The script uses Apple's `swiftc` and `codesign` command-line tools to install `~/Applications/Lark Helper.app`. Each CLI request registers a temporary user LaunchAgent, starts the app through `launchd`, and unregisters the job when done. The helper has no GUI and exits after the command. `battery --watch` keeps it alive only while watching. It starts the existing Python toolkit with a small allowlist of commands and returns stdout, stderr, and the exit code through a private temporary directory. No sudo or persistent daemon is needed. `lark devices` remains a direct enumeration command.

In System Settings → Privacy & Security → Input Monitoring, enable **Lark Helper**. If it is absent and the panel offers a `+` button, add `~/Applications/Lark Helper.app` manually, then retry. Keep Ghostty and Kitty ungranted. Closing a permission dialog can suppress further prompts; Apple's targeted reset is `tccutil reset ListenEvent com.akurin.lark.helper` (no sudo), which clears only this app's Input Monitoring decision. If a prompt names a terminal, do not grant it; report that attribution result. The helper routes commands automatically when installed; set `LARK_DIRECT=1` for direct-access diagnostics. Rebuilding a signed app can require a new macOS permission grant, so the helper installer skips unchanged builds. The uninstall script removes this project's helper app too. The LaunchAgent's permission attribution requires validation on your Mac; the Linux test runner cannot check macOS TCC behavior.

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

If `lark doctor` sees the receiver but cannot read it, macOS may require **Input Monitoring** for the responsible app. The CLI prefers vendor HID collections `0xFF90` and `0xFF82`, but some Macs enumerate only a Generic Desktop collection for this receiver. It tries the enumerated path and, for a single-interface receiver, the same `open(VID, PID)` call as the verified hardware probe. Either may require Input Monitoring for the terminal when running directly. Install the optional helper above to keep that permission off general-purpose terminals. Close HollyAudio and other receiver controllers if the device is busy. Unplug and replug after a disconnect. Power on and pair a transmitter if both microphones show disconnected. No sudo is needed.

## Protocol and source attribution

Status framing, command `0x10`, and battery offsets were verified with the user's physical receiver. The captured 16-byte payload is `01 00 4c 00 00 00 00 05 00 00 00 00 00 00 04 00` and decodes to Mic 1 at 76%, Mic 2 disconnected. Firmware command `0x41`, noise byte 6, mute/charging bits in byte 15, and noise commands `0x19` and `0x06` were derived from [LARK Flow Bridge protocol notes](https://github.com/geekyshubham/lark-flow-bridge/blob/main/docs/HOW_IT_WORKS.md) and [Swift implementation](https://github.com/geekyshubham/lark-flow-bridge/blob/main/LARKFlowBridge.swift). The captured `0x04` is in byte 14, so it does **not** confirm the upstream charging bit at byte 15. That project is MIT licensed. This package uses its documented packet facts and an independent Python implementation; no upstream source was copied.

Run tests with `python3 -m unittest discover -s tests -v` after installing, or `.venv/bin/python -m unittest discover -s tests -v`.

GitHub Actions runs these tests and builds/signs the optional helper on an Apple Silicon macOS runner. CI has no physical receiver or Input Monitoring grant, so it cannot verify live HID access or macOS permission attribution.
