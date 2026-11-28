package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const version = "0.2.0-go"

type options struct {
	command  string
	action   string
	level    int
	json     bool
	watch    bool
	interval time.Duration
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var larkErr *larkError
	if errors.As(err, &larkErr) {
		return larkErr.Code
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	return 4
}

func micBattery(w io.Writer, s status) {
	for _, m := range s.Microphones {
		value := "Disconnected"
		if m.Connected {
			value = "Unknown"
			if m.Battery != nil {
				value = fmt.Sprintf("%d%%", *m.Battery)
			}
		}
		fmt.Fprintf(w, "Mic %d: %s\n", m.ID, value)
	}
}

func micState(w io.Writer, s status, field string) {
	for _, m := range s.Microphones {
		value := "Disconnected"
		if m.Connected {
			state := m.Muted
			if field == "charging" {
				state = m.Charging
			}
			value = "No"
			if state != nil && *state {
				value = "Yes"
			}
		}
		fmt.Fprintf(w, "Mic %d: %s\n", m.ID, value)
	}
}

func noise(s status) string {
	if s.NoiseLevel == nil {
		return "Unknown"
	}
	if *s.NoiseLevel == 0 {
		return "Off"
	}
	return fmt.Sprintf("Level %d", *s.NoiseLevel)
}

func confirmNoise(ctx context.Context, r *receiver, target int) error {
	for i := 0; i < 4; i++ {
		if err := waitContext(ctx, 100*time.Millisecond); err != nil {
			return err
		}
		s, err := r.status()
		if err != nil {
			return err
		}
		if s.NoiseLevel != nil && *s.NoiseLevel == target {
			return nil
		}
	}
	return &larkError{4, "noise setting was not confirmed by the receiver"}
}

func setNoise(ctx context.Context, r *receiver, action string, level int) (status, error) {
	s, err := r.status()
	if err != nil {
		return s, err
	}
	if !s.Microphones[0].Connected && !s.Microphones[1].Connected {
		return s, &larkError{4, "power on a transmitter before changing noise cancellation"}
	}
	if s.NoiseLevel == nil {
		return s, &larkError{5, "unknown noise state; refusing to change it"}
	}
	switch action {
	case "off":
		if *s.NoiseLevel != 0 {
			if err := r.send(0x19, []byte{2}); err != nil {
				return s, err
			}
			if err := confirmNoise(ctx, r, 0); err != nil {
				return s, err
			}
		}
	case "on":
		if *s.NoiseLevel == 0 {
			if err := r.send(0x19, []byte{2}); err != nil {
				return s, err
			}
			confirmed := false
			for i := 0; i < 4; i++ {
				if err := waitContext(ctx, 100*time.Millisecond); err != nil {
					return s, err
				}
				s, err = r.status()
				if err != nil {
					return s, err
				}
				if s.NoiseLevel != nil && *s.NoiseLevel > 0 {
					confirmed = true
					break
				}
			}
			if !confirmed {
				return s, &larkError{4, "noise cancellation did not turn on"}
			}
		}
	case "level":
		if *s.NoiseLevel == 0 {
			if err := r.send(0x19, []byte{2}); err != nil {
				return s, err
			}
			if err := waitContext(ctx, 80*time.Millisecond); err != nil {
				return s, err
			}
		}
		if err := r.send(0x06, []byte{byte(level)}); err != nil {
			return s, err
		}
		if err := confirmNoise(ctx, r, level); err != nil {
			return s, err
		}
	}
	return r.status()
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func runSelected(ctx context.Context, o options, backend hidBackend, out, stderr io.Writer) int {
	entries, err := enumerate(backend)
	if err != nil {
		fmt.Fprintf(stderr, "lark: %v\n", err)
		return exitCode(err)
	}
	if o.command == "devices" {
		if len(entries) == 0 {
			fmt.Fprintln(out, "No LARK A2 HID interfaces found")
			return 2
		}
		for _, entry := range entries {
			fmt.Fprintln(out, entry)
		}
		return 0
	}
	if o.command == "doctor" {
		fmt.Fprintln(out, "Hollyland LARK A2 diagnostic")
		if len(entries) == 0 {
			fmt.Fprintln(out, "Receiver: absent. Plug in the LARK A2 USB-C receiver (3547:0411).")
			return 2
		}
		pages := make([]string, len(entries))
		vendor := false
		for i, entry := range entries {
			pages[i] = fmt.Sprintf("0x%04x", entry.UsagePage)
			vendor = vendor || entry.UsagePage == 0xff90 || entry.UsagePage == 0xff82
		}
		fmt.Fprintf(out, "Receiver: %d HID interface(s) found; usage pages: %s\n", len(entries), strings.Join(pages, ", "))
		if !vendor {
			fmt.Fprintln(out, "No separate vendor collection is exposed; the receiver's control reports may use its Generic Desktop interface.")
		}
	}
	r, err := openReceiver(backend)
	if err != nil {
		return reportError(o.command, err, out, stderr)
	}
	defer r.close()
	if o.command == "battery" {
		for {
			if ctx.Err() != nil {
				return 130
			}
			s, err := r.status()
			if err != nil {
				return reportError(o.command, err, out, stderr)
			}
			if o.json {
				if err := json.NewEncoder(out).Encode(s.batteryJSON()); err != nil {
					fmt.Fprintf(stderr, "lark: %v\n", err)
					return 4
				}
			} else {
				micBattery(out, s)
			}
			if !o.watch {
				return 0
			}
			if waitContext(ctx, o.interval) != nil {
				return 130
			}
		}
	}
	switch o.command {
	case "doctor", "status", "nc", "mute", "charging":
		if o.command == "nc" && o.action != "status" {
			s, err := setNoise(ctx, r, o.action, o.level)
			if err != nil {
				return reportError(o.command, err, out, stderr)
			}
			fmt.Fprintf(out, "Noise cancellation: %s\n", noise(s))
			return 0
		}
		s, err := r.status()
		if err != nil {
			return reportError(o.command, err, out, stderr)
		}
		switch o.command {
		case "doctor":
			fmt.Fprintln(out, "HID status read: OK")
			micBattery(out, s)
			if !s.Microphones[0].Connected && !s.Microphones[1].Connected {
				fmt.Fprintln(out, "Power on and pair a transmitter to read its battery.")
			}
		case "status":
			micBattery(out, s)
			fmt.Fprintf(out, "Noise cancellation: %s\nMuted:\n", noise(s))
			micState(out, s, "muted")
			fmt.Fprintln(out, "Charging:")
			micState(out, s, "charging")
		case "nc":
			fmt.Fprintf(out, "Noise cancellation: %s\n", noise(s))
		case "mute":
			micState(out, s, "muted")
		case "charging":
			micState(out, s, "charging")
		}
	case "raw":
		payload, err := r.exchange(statusCommand, nil)
		if err != nil {
			return reportError(o.command, err, out, stderr)
		}
		for i, b := range payload {
			if i > 0 {
				fmt.Fprint(out, " ")
			}
			fmt.Fprintf(out, "%02x", b)
		}
		fmt.Fprintln(out)
	case "info", "firmware":
		if o.command == "info" {
			fmt.Fprintln(out, "Hollyland LARK A2 USB-C receiver (3547:0411)")
		}
		value, err := r.firmware()
		if err != nil {
			return reportError(o.command, err, out, stderr)
		}
		fmt.Fprintf(out, "Firmware: %s\n", value)
	}
	return 0
}

func reportError(command string, err error, out, stderr io.Writer) int {
	if command == "doctor" {
		fmt.Fprintf(out, "HID status read: %v\n", err)
		fmt.Fprintln(out, "Check Input Monitoring for Lark Helper in System Settings → Privacy & Security. Close other receiver controllers and reconnect USB if needed.")
	} else {
		fmt.Fprintf(stderr, "lark: %v\n", err)
	}
	return exitCode(err)
}
