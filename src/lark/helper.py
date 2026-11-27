"""One-shot client for the Launch Services app that owns macOS HID permission."""
import json
import os
from pathlib import Path
import plistlib
import subprocess
import sys
import tempfile
import time
import uuid


APP = Path.home() / "Applications" / "Lark Helper.app"
POLL_SECONDS = 0.05
START_SECONDS = 15
ONESHOT_SECONDS = 120


def available() -> bool:
    return (sys.platform == "darwin"
            and os.environ.get("LARK_IN_HELPER") != "1"
            and os.environ.get("LARK_DIRECT") != "1"
            and (APP / "Contents" / "MacOS" / "LarkHelper").is_file())


def _drain(path: Path, offset: int, stream) -> int:
    with path.open("rb") as handle:
        handle.seek(offset)
        chunk = handle.read()
    if chunk:
        if hasattr(stream, "buffer"):
            stream.buffer.write(chunk)
        else:
            stream.write(chunk.decode("utf-8", errors="replace"))
        stream.flush()
    return offset + len(chunk)


def run(argv: list[str], *, watch: bool = False) -> int:
    with tempfile.TemporaryDirectory(prefix="lark-helper-") as directory:
        root = Path(directory)
        (root / "request.json").write_text(json.dumps({"argv": argv}), encoding="utf-8")
        for name in ("stdout", "stderr"):
            (root / name).touch(mode=0o600)
        label = f"com.akurin.lark.helper.request.{uuid.uuid4().hex}"
        target = f"gui/{os.getuid()}/{label}"
        agent = root / "agent.plist"
        with agent.open("wb") as handle:
            plistlib.dump({
                "Label": label,
                "ProgramArguments": [str(APP / "Contents" / "MacOS" / "LarkHelper"), str(root)],
                "RunAtLoad": False,
                "KeepAlive": False,
            }, handle)
        try:
            launched = subprocess.run(
                ["/bin/launchctl", "bootstrap", f"gui/{os.getuid()}", str(agent)],
                capture_output=True, text=True, check=False, timeout=15,
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            print(f"lark: could not register Lark Helper: {exc}", file=sys.stderr)
            return 4
        if launched.returncode:
            print(f"lark: could not register Lark Helper: {launched.stderr.strip()}", file=sys.stderr)
            return 4
        try:
            kicked = subprocess.run(
                ["/bin/launchctl", "kickstart", target],
                capture_output=True, text=True, check=False, timeout=15,
            )
            if kicked.returncode:
                print(f"lark: could not start Lark Helper: {kicked.stderr.strip()}", file=sys.stderr)
                return 4
            offsets = {"stdout": 0, "stderr": 0}
            started = time.monotonic()
            ready_at = None
            while True:
                offsets["stdout"] = _drain(root / "stdout", offsets["stdout"], sys.stdout)
                offsets["stderr"] = _drain(root / "stderr", offsets["stderr"], sys.stderr)
                result = root / "result.json"
                if result.exists():
                    try:
                        code = json.loads(result.read_text(encoding="utf-8"))["exit_code"]
                        return int(code)
                    except (OSError, ValueError, KeyError, TypeError):
                        print("lark: invalid helper result", file=sys.stderr)
                        return 5
                now = time.monotonic()
                if ready_at is None and (root / "ready").exists():
                    ready_at = now
                if ready_at is None and now - started > START_SECONDS:
                    print("lark: Lark Helper did not start", file=sys.stderr)
                    return 4
                if ready_at is not None and not watch and now - ready_at > ONESHOT_SECONDS:
                    (root / "cancel").touch()
                    print("lark: Lark Helper timed out", file=sys.stderr)
                    return 6
                time.sleep(POLL_SECONDS)
        except KeyboardInterrupt:
            (root / "cancel").touch()
            # Give the helper time to terminate its watch subprocess before
            # removing the private request directory.
            deadline = time.monotonic() + 2
            while time.monotonic() < deadline and not (root / "result.json").exists():
                time.sleep(POLL_SECONDS)
            print(file=sys.stderr)
            return 130
        except (OSError, subprocess.TimeoutExpired) as exc:
            print(f"lark: could not start Lark Helper: {exc}", file=sys.stderr)
            return 4
        finally:
            try:
                subprocess.run(
                    ["/bin/launchctl", "bootout", target],
                    capture_output=True, text=True, check=False, timeout=15,
                )
            except (OSError, subprocess.TimeoutExpired):
                pass
