package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var capturedPayload = []byte{1, 0, 0x4c, 0, 0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 4, 0}

func responsePacket(payload []byte, withID bool) []byte {
	packet := []byte{0xbb, 0xdd, statusCommand, 0x80, 0, byte(len(payload))}
	packet = append(packet, payload...)
	if withID { packet = append([]byte{reportID}, packet...) }
	return packet
}

func TestCapturedBattery(t *testing.T) {
	for _, withID := range []bool{false, true} {
		payload, err := parseResponse(responsePacket(capturedPayload, withID), statusCommand)
		if err != nil || !reflect.DeepEqual(payload, capturedPayload) { t.Fatalf("payload=%v err=%v", payload, err) }
		s, err := decodeStatus(payload)
		if err != nil { t.Fatal(err) }
		if !s.Microphones[0].Connected || *s.Microphones[0].Battery != 76 || s.Microphones[1].Connected || s.Microphones[1].Battery != nil { t.Fatalf("wrong battery: %+v", s) }
		data, err := json.Marshal(s.batteryJSON())
		if err != nil { t.Fatal(err) }
		want := `{"receiver_connected":true,"microphones":[{"id":1,"connected":true,"battery":76},{"id":2,"connected":false,"battery":null}]}`
		if string(data) != want { t.Fatalf("json=%s", data) }
	}
}

func TestBatteryBounds(t *testing.T) {
	payload := append([]byte(nil), capturedPayload...)
	payload[0], payload[1], payload[2], payload[3] = 1, 1, 0, 100
	s, err := decodeStatus(payload)
	if err != nil { t.Fatal(err) }
	if *s.Microphones[0].Battery != 0 || *s.Microphones[1].Battery != 100 { t.Fatalf("bounds: %+v", s) }
	payload[2] = 101
	s, err = decodeStatus(payload)
	if err != nil || s.Microphones[0].Battery != nil { t.Fatalf("invalid battery should be nil: %+v %v", s, err) }
}

func TestMalformedPackets(t *testing.T) {
	for _, packet := range [][]byte{
		{}, {0xbb, 0xdd, statusCommand},
		{0xbb, 0xdd, 0x11, 0x80, 0, 0},
		{0xbb, 0xdd, statusCommand, 0x80, 0, 16, 1},
	} {
		if _, err := parseResponse(packet, statusCommand); err == nil { t.Fatalf("accepted %x", packet) }
	}
	if _, err := decodeStatus(capturedPayload[:15]); err == nil { t.Fatal("accepted short payload") }
	payload := append([]byte(nil), capturedPayload...)
	payload[0] = 2
	if _, err := decodeStatus(payload); err == nil || !strings.Contains(err.Error(), "connection") { t.Fatalf("bad flag: %v", err) }
}

func TestRequest(t *testing.T) {
	packet, err := makeRequest(statusCommand, nil)
	if err != nil { t.Fatal(err) }
	if len(packet) != 64 || !reflect.DeepEqual(packet[:7], []byte{0x55, 0xaa, 0xdd, 0x10, 0x40, 0, 0}) { t.Fatalf("request: %x", packet) }
}
