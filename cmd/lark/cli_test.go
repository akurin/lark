package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBareStateCommands(t *testing.T) {
	for _, command := range []string{"nc", "mute", "charging"} {
		d := &fakeDevice{packet: responsePacket(capturedPayload, false)}
		b := &fakeBackend{entries: []deviceInfo{{"path", 1, 2}}, dev: d}
		var out, stderr bytes.Buffer
		code := runCommand(context.Background(), []string{command}, b, &out, &stderr)
		if code != 0 || len(d.writes) != 1 || stderr.Len() != 0 {
			t.Fatalf("%s: code=%d writes=%d stderr=%q", command, code, len(d.writes), stderr.String())
		}
		if command == "nc" && out.String() != "Noise cancellation: Off\n" {
			t.Fatalf("nc: %q", out.String())
		}
		if command != "nc" && !strings.Contains(out.String(), "Mic 1: No") {
			t.Fatalf("%s: %q", command, out.String())
		}
	}
}

func TestCommandHelpDoesNotOpenHID(t *testing.T) {
	b := &fakeBackend{enumerateErr: errors.New("HID should not be used")}
	for _, args := range [][]string{{"--help"}, {"nc", "--help"}, {"help", "nc"}, {"battery", "--help"}, {"nc", "level", "--help"}} {
		var out, stderr bytes.Buffer
		if code := runCommand(context.Background(), args, b, &out, &stderr); code != 0 || !strings.Contains(out.String(), "Usage:") || stderr.Len() != 0 {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, out.String(), stderr.String())
		}
	}
}

func TestInvalidActionSuggestsHelp(t *testing.T) {
	var out, stderr bytes.Buffer
	code := runCommand(context.Background(), []string{"nc", "louder"}, &fakeBackend{}, &out, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "--help") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestInvalidLevelAndBatteryInterval(t *testing.T) {
	for _, args := range [][]string{{"nc", "level", "4"}, {"battery", "--interval", "0"}} {
		var out, stderr bytes.Buffer
		code := runCommand(context.Background(), args, &fakeBackend{enumerateErr: errors.New("HID should not be used")}, &out, &stderr)
		if code != 2 || stderr.Len() == 0 {
			t.Fatalf("%v: code=%d stderr=%q", args, code, stderr.String())
		}
	}
}
