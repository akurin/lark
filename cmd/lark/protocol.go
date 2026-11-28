package main

import (
	"encoding/binary"
	"fmt"
)

const (
	reportID        = 0x55
	reportSize      = 64
	statusCommand   = 0x10
	firmwareCommand = 0x41
)

type microphone struct {
	ID        int   `json:"id"`
	Connected bool  `json:"connected"`
	Battery   *int  `json:"battery"`
	Muted     *bool `json:"-"`
	Charging  *bool `json:"-"`
}

type status struct {
	Microphones [2]microphone `json:"microphones"`
	NoiseLevel  *int          `json:"-"`
}

type batteryJSON struct {
	ReceiverConnected bool          `json:"receiver_connected"`
	Microphones       [2]microphone `json:"microphones"`
}

func (s status) batteryJSON() batteryJSON {
	return batteryJSON{true, s.Microphones}
}

func makeRequest(command byte, payload []byte) ([]byte, error) {
	if len(payload) > reportSize-7 {
		return nil, fmt.Errorf("payload too long")
	}
	packet := make([]byte, reportSize)
	copy(packet, []byte{reportID, 0xaa, 0xdd, command, 0x40, 0, byte(len(payload))})
	copy(packet[7:], payload)
	return packet, nil
}

func parseResponse(data []byte, command byte) ([]byte, error) {
	if len(data) > 0 && data[0] == reportID {
		data = data[1:]
	}
	if len(data) < 6 {
		return nil, fmt.Errorf("truncated response header")
	}
	if data[0] != 0xbb || data[1] != 0xdd || data[2] != command || data[3] != 0x80 {
		return nil, fmt.Errorf("unexpected response header, command, or destination")
	}
	length := int(binary.BigEndian.Uint16(data[4:6]))
	if len(data) < 6+length {
		return nil, fmt.Errorf("truncated response payload: expected %d bytes", length)
	}
	return data[6 : 6+length], nil
}

func decodeStatus(payload []byte) (status, error) {
	var result status
	if len(payload) != 16 {
		return result, fmt.Errorf("status payload must be 16 bytes, got %d", len(payload))
	}
	for i := 0; i < 2; i++ {
		if payload[i] != 0 && payload[i] != 1 {
			return result, fmt.Errorf("invalid Mic %d connection flag", i+1)
		}
		m := microphone{ID: i + 1, Connected: payload[i] == 1}
		if m.Connected {
			if payload[i+2] <= 100 {
				value := int(payload[i+2])
				m.Battery = &value
			}
			muted, charging := payload[15]&(1<<i) != 0, payload[15]&(4<<i) != 0
			m.Muted, m.Charging = &muted, &charging
		}
		result.Microphones[i] = m
	}
	if payload[6] <= 3 {
		value := int(payload[6])
		result.NoiseLevel = &value
	}
	return result, nil
}

func decodeFirmware(payload []byte) (string, error) {
	if len(payload) < 6 {
		return "", fmt.Errorf("firmware payload must contain at least 6 bytes")
	}
	return fmt.Sprintf("%d.%d.%d.%d%d%d", payload[0], payload[1], payload[2], payload[3], payload[4], payload[5]), nil
}
