package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"
)

const version = "0.2.0-go"

type options struct {
	command string
	action string
	level int
	json bool
	watch bool
	interval time.Duration
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Hollyland LARK A2 USB HID toolkit")
	fmt.Fprintln(w, "Usage: lark <command> [options]")
	fmt.Fprintln(w, "Commands: status, battery, info, firmware, devices, doctor, raw, nc, mute, charging")
	fmt.Fprintln(w, "Battery options: --json, --watch, --interval SECONDS (default: 5)")
	fmt.Fprintln(w, "NC actions: status, on, off, level 1|2|3")
	fmt.Fprintln(w, "Mute and charging actions: status")
}

func parseArgs(args []string) (options, error) {
	o := options{interval: 5*time.Second}
	if len(args) == 0 { return o, fmt.Errorf("command required") }
	o.command = args[0]
	switch o.command {
	case "status", "info", "firmware", "devices", "doctor", "raw":
		if len(args) != 1 { return o, fmt.Errorf("unexpected arguments for %s", o.command) }
	case "battery":
		flags := flag.NewFlagSet("battery", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		flags.BoolVar(&o.json, "json", false, "scriptable JSON")
		flags.BoolVar(&o.watch, "watch", false, "poll continuously")
		var seconds float64
		flags.Float64Var(&seconds, "interval", 5, "seconds between samples")
		if err := flags.Parse(args[1:]); err != nil { return o, err }
		if flags.NArg() != 0 { return o, fmt.Errorf("unexpected battery arguments") }
		if seconds <= 0 || seconds > 86400 { return o, fmt.Errorf("--interval must be positive and at most 86400 seconds") }
		o.interval = time.Duration(seconds * float64(time.Second))
		if o.interval < time.Millisecond { return o, fmt.Errorf("--interval must be at least 0.001 seconds") }
	case "nc":
		if len(args) < 2 { return o, fmt.Errorf("nc action required") }
		o.action = args[1]
		switch o.action {
		case "status", "on", "off":
			if len(args) != 2 { return o, fmt.Errorf("unexpected nc arguments") }
		case "level":
			if len(args) != 3 || (args[2] != "1" && args[2] != "2" && args[2] != "3") { return o, fmt.Errorf("nc level must be 1, 2, or 3") }
			o.level = int(args[2][0] - '0')
		default: return o, fmt.Errorf("unknown nc action: %s", o.action)
		}
	case "mute", "charging":
		if len(args) != 2 || args[1] != "status" { return o, fmt.Errorf("usage: lark %s status", o.command) }
		o.action = "status"
	default: return o, fmt.Errorf("unknown command: %s", o.command)
	}
	return o, nil
}

func exitCode(err error) int {
	if err == nil { return 0 }
	var larkErr *larkError
	if errors.As(err, &larkErr) { return larkErr.Code }
	if errors.Is(err, context.Canceled) { return 130 }
	return 4
}

func micBattery(w io.Writer, s status) {
	for _, m := range s.Microphones {
		value := "Disconnected"
		if m.Connected { value = "Unknown"; if m.Battery != nil { value = fmt.Sprintf("%d%%", *m.Battery) } }
		fmt.Fprintf(w, "Mic %d: %s\n", m.ID, value)
	}
}

func micState(w io.Writer, s status, field string) {
	for _, m := range s.Microphones {
		value := "Disconnected"
		if m.Connected {
			state := m.Muted
			if field == "charging" { state = m.Charging }
			value = "No"
			if state != nil && *state { value = "Yes" }
		}
		fmt.Fprintf(w, "Mic %d: %s\n", m.ID, value)
	}
}

func noise(s status) string {
	if s.NoiseLevel == nil { return "Unknown" }
	if *s.NoiseLevel == 0 { return "Off" }
	return fmt.Sprintf("Level %d", *s.NoiseLevel)
}

func confirmNoise(ctx context.Context, r *receiver, target int) error {
	for i := 0; i < 4; i++ {
		if err := waitContext(ctx, 100*time.Millisecond); err != nil { return err }
		s, err := r.status()
		if err != nil { return err }
		if s.NoiseLevel != nil && *s.NoiseLevel == target { return nil }
	}
	return &larkError{4, "noise setting was not confirmed by the receiver"}
}

func setNoise(ctx context.Context, r *receiver, action string, level int) (status, error) {
	s, err := r.status()
	if err != nil { return s, err }
	if !s.Microphones[0].Connected && !s.Microphones[1].Connected { return s, &larkError{4, "power on a transmitter before changing noise cancellation"} }
	if s.NoiseLevel == nil { return s, &larkError{5, "unknown noise state; refusing to change it"} }
	switch action {
	case "off":
		if *s.NoiseLevel != 0 {
			if err := r.send(0x19, []byte{2}); err != nil { return s, err }
			if err := confirmNoise(ctx, r, 0); err != nil { return s, err }
		}
	case "on":
		if *s.NoiseLevel == 0 {
			if err := r.send(0x19, []byte{2}); err != nil { return s, err }
			confirmed := false
			for i := 0; i < 4; i++ {
				if err := waitContext(ctx, 100*time.Millisecond); err != nil { return s, err }
				s, err = r.status(); if err != nil { return s, err }
				if s.NoiseLevel != nil && *s.NoiseLevel > 0 { confirmed = true; break }
			}
			if !confirmed { return s, &larkError{4, "noise cancellation did not turn on"} }
		}
	case "level":
		if *s.NoiseLevel == 0 {
			if err := r.send(0x19, []byte{2}); err != nil { return s, err }
			if err := waitContext(ctx, 80*time.Millisecond); err != nil { return s, err }
		}
		if err := r.send(0x06, []byte{byte(level)}); err != nil { return s, err }
		if err := confirmNoise(ctx, r, level); err != nil { return s, err }
	}
	return r.status()
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select { case <-ctx.Done(): return ctx.Err(); case <-timer.C: return nil }
}

func runCommand(ctx context.Context, args []string, backend hidBackend, out, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" { usage(out); return 0 }
	if args[0] == "--version" { fmt.Fprintln(out, version); return 0 }
	o, err := parseArgs(args)
	if err != nil { fmt.Fprintf(stderr, "lark: %v\n", err); return 2 }
	entries, err := enumerate(backend)
	if err != nil { fmt.Fprintf(stderr, "lark: %v\n", err); return exitCode(err) }
	if o.command == "devices" {
		if len(entries) == 0 { fmt.Fprintln(out, "No LARK A2 HID interfaces found"); return 2 }
		for _, entry := range entries { fmt.Fprintln(out, entry) }
		return 0
	}
	if o.command == "doctor" {
		fmt.Fprintln(out, "Hollyland LARK A2 diagnostic")
		if len(entries) == 0 { fmt.Fprintln(out, "Receiver: absent. Plug in the LARK A2 USB-C receiver (3547:0411)."); return 2 }
		pages := make([]string, len(entries))
		vendor := false
		for i, entry := range entries { pages[i] = fmt.Sprintf("0x%04x", entry.UsagePage); vendor = vendor || entry.UsagePage == 0xff90 || entry.UsagePage == 0xff82 }
		fmt.Fprintf(out, "Receiver: %d HID interface(s) found; usage pages: %s\n", len(entries), strings.Join(pages, ", "))
		if !vendor { fmt.Fprintln(out, "No separate vendor collection is exposed; the receiver's control reports may use its Generic Desktop interface.") }
	}
	r, err := openReceiver(backend)
	if err != nil { return reportError(o.command, err, out, stderr) }
	defer r.close()
	if o.command == "battery" {
		for {
			if ctx.Err() != nil { return 130 }
			s, err := r.status()
			if err != nil { return reportError(o.command, err, out, stderr) }
			if o.json { if err := json.NewEncoder(out).Encode(s.batteryJSON()); err != nil { fmt.Fprintf(stderr, "lark: %v\n", err); return 4 } } else { micBattery(out, s) }
			if !o.watch { return 0 }
			if waitContext(ctx, o.interval) != nil { return 130 }
		}
	}
	switch o.command {
	case "doctor", "status", "nc", "mute", "charging":
		if o.command == "nc" && o.action != "status" {
			s, err := setNoise(ctx, r, o.action, o.level)
			if err != nil { return reportError(o.command, err, out, stderr) }
			fmt.Fprintf(out, "Noise cancellation: %s\n", noise(s)); return 0
		}
		s, err := r.status()
		if err != nil { return reportError(o.command, err, out, stderr) }
		switch o.command {
		case "doctor":
			fmt.Fprintln(out, "HID status read: OK"); micBattery(out, s)
			if !s.Microphones[0].Connected && !s.Microphones[1].Connected { fmt.Fprintln(out, "Power on and pair a transmitter to read its battery.") }
		case "status":
			micBattery(out, s); fmt.Fprintf(out, "Noise cancellation: %s\nMuted:\n", noise(s)); micState(out, s, "muted"); fmt.Fprintln(out, "Charging:"); micState(out, s, "charging")
		case "nc": fmt.Fprintf(out, "Noise cancellation: %s\n", noise(s))
		case "mute": micState(out, s, "muted")
		case "charging": micState(out, s, "charging")
		}
	case "raw":
		payload, err := r.exchange(statusCommand, nil)
		if err != nil { return reportError(o.command, err, out, stderr) }
		for i, b := range payload { if i > 0 { fmt.Fprint(out, " ") }; fmt.Fprintf(out, "%02x", b) }; fmt.Fprintln(out)
	case "info", "firmware":
		if o.command == "info" { fmt.Fprintln(out, "Hollyland LARK A2 USB-C receiver (3547:0411)") }
		value, err := r.firmware()
		if err != nil { return reportError(o.command, err, out, stderr) }
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

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--helper-request" { os.Exit(runHelper(os.Args[2])) }
	args := os.Args[1:]
	if len(args) > 0 && args[0] != "devices" && args[0] != "--help" && args[0] != "-h" && args[0] != "help" && args[0] != "--version" && os.Getenv("LARK_DIRECT") != "1" {
		app, err := helperApp()
		if err == nil { os.Exit(runViaHelper(app, args, os.Stdout, os.Stderr)) }
		if runtime.GOOS == "darwin" {
			fmt.Fprintf(os.Stderr, "lark: Lark Helper is unavailable: %v; run scripts/install.sh or set LARK_DIRECT=1 for direct access\n", err)
			os.Exit(4)
		}
	}
	os.Exit(runCommand(context.Background(), args, nativeHID{}, os.Stdout, os.Stderr))
}
