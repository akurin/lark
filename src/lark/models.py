from dataclasses import dataclass


@dataclass(frozen=True)
class Microphone:
    id: int
    connected: bool
    battery: int | None
    muted: bool | None
    charging: bool | None


@dataclass(frozen=True)
class Status:
    microphones: tuple[Microphone, Microphone]
    noise_level: int | None

    def battery_json(self) -> dict:
        return {
            "receiver_connected": True,
            "microphones": [
                {"id": m.id, "connected": m.connected, "battery": m.battery}
                for m in self.microphones
            ],
        }
