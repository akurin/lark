# LARK A2 CLI

A short-lived Go command-line toolkit for the Hollyland LARK A2 USB-C receiver (`3547:0411`) on Apple Silicon macOS. It uses [go-hid](https://github.com/sstallion/go-hid), a cgo binding to HIDAPI. It needs no Python runtime, Swift runtime, HollyAudio app, sudo, or persistent daemon.

## Install

Install Go and Xcode Command Line Tools on the Mac, then run from this repository:

```sh
go test ./...
./scripts/install.sh
lark doctor
lark battery
lark battery --json
```

The idempotent installer builds `build/lark`, installs an ad hoc signed `~/Applications/Lark Helper.app`, and links `~/.local/bin/lark` to the binary. It reports if `~/.local/bin` is missing from `PATH` and never changes shell startup files. It can replace this project's previous Python CLI link and helper app. Run `./scripts/uninstall.sh` to remove the Go CLI link, binary, and app. An old Python virtual environment, if present, can be removed separately after verification.

### Input Monitoring

The terminal command registers a temporary user LaunchAgent, starts the same Go executable inside `Lark Helper.app`, and reads its output from a private temporary directory. The app talks to HID directly and exits after the command. `battery --watch` keeps it alive only while watching. `lark devices` only enumerates interfaces and runs directly. No background daemon remains after a command.

In System Settings → Privacy & Security → Input Monitoring, grant access to **Lark Helper**. You can leave Ghostty and Kitty ungranted. If no permission prompt appears, add `~/Applications/Lark Helper.app` with the `+` button. A closed or denied dialog can be reset with `tccutil reset ListenEvent com.akurin.lark.helper`, then relaunch the command. A rebuild may require a fresh grant because the ad hoc signature changes with the executable. Set `LARK_DIRECT=1` only when intentionally testing direct terminal HID access.

## Commands

| Command | Result | Evidence |
| --- | --- | --- |
| `lark battery`, `lark status` | Batteries, connection, and additional state | Battery and connection hardware captured; other fields from upstream |
| `lark battery --json` | Scriptable JSON object | Hardware captured battery format |
| `lark battery --watch --interval 5` | Poll until Ctrl+C; JSON mode emits one object per line | Same status request |
| `lark devices`, `lark doctor` | Enumerate interfaces and diagnose access | HIDAPI |
| `lark raw` | Hex status payload; read-only | Hardware captured status command |
| `lark info`, `lark firmware` | Receiver identity and firmware | Firmware command from upstream source |
| `lark nc status`, `lark mute status`, `lark charging status` | Read-only states | Field positions from upstream source |
| `lark nc on`, `lark nc off`, `lark nc level 2` | Set noise cancellation and check reported state | Write packets tested by upstream on its receiver; **not tested on this receiver** |

`nc level` accepts 1, 2, or 3. The noise switch is a toggle, so the CLI reads the current state and confirms changes with another read. Firmware updates and undocumented writes are not implemented. `raw` sends only the known status request.

Example JSON:

```json
{"receiver_connected":true,"microphones":[{"id":1,"connected":true,"battery":76},{"id":2,"connected":false,"battery":null}]}
```

JSON data goes to stdout and diagnostics to stderr. Exit codes: `0` success, `2` receiver absent or invalid arguments, `3` HID permission denied, `4` other HID I/O or unconfirmed control, `5` invalid response, `6` timeout, `130` interrupted. A connected microphone with an invalid battery byte reports `null` / `Unknown`.

## Troubleshooting

If `lark doctor` sees the receiver but cannot read it, check Input Monitoring for Lark Helper, close other receiver controllers, and reconnect USB. Some receivers expose only a Generic Desktop HID collection. The CLI tries the enumerated path and, for a single interface, the verified vendor/product open method. If both microphones show disconnected, power on and pair a transmitter. No sudo is needed.

## Protocol and testing

Status framing, command `0x10`, and battery offsets were verified with the physical receiver using the earlier Python probe. The captured 16-byte payload is `01 00 4c 00 00 00 00 05 00 00 00 00 00 00 04 00`; it means Mic 1 is at 76% and Mic 2 is disconnected. Go decoding is tested against that captured packet and mocked HID devices. Live Go HID access and macOS permission attribution require a Mac with the receiver.

Firmware command `0x41`, noise byte 6, mute/charging bits in byte 15, and noise commands `0x19` and `0x06` come from [LARK Flow Bridge protocol notes](https://github.com/geekyshubham/lark-flow-bridge/blob/main/docs/HOW_IT_WORKS.md) and [Swift implementation](https://github.com/geekyshubham/lark-flow-bridge/blob/main/LARKFlowBridge.swift). The captured `0x04` is in byte 14 and does not confirm the upstream charging bit at byte 15. The upstream project is MIT licensed; this is an independent Go implementation of its documented packet facts. The Go HID binding is BSD-2-Clause licensed.

Run mocked tests with `go test ./...`. GitHub Actions runs the tests and builds/signs the app on an Apple Silicon macOS runner. CI has no physical receiver or Input Monitoring grant.
