package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

type commandExit struct{ code int }

func (e *commandExit) Error() string { return fmt.Sprintf("command exited with status %d", e.code) }

func result(code int) error {
	if code == 0 {
		return nil
	}
	return &commandExit{code: code}
}

func executeCLI(ctx context.Context, args []string, backend hidBackend, out, stderr io.Writer, route bool) int {
	root := newRootCommand(ctx, args, backend, out, stderr, route)
	root.SetArgs(args)
	root.SetOut(out)
	root.SetErr(stderr)
	cmd, err := root.ExecuteC()
	if err == nil {
		return 0
	}
	var completed *commandExit
	if errors.As(err, &completed) {
		return completed.code
	}
	fmt.Fprintf(stderr, "lark: %v\n", err)
	if cmd != nil && cmd != root {
		fmt.Fprintf(stderr, "Run '%s --help' for usage.\n", cmd.CommandPath())
	} else {
		fmt.Fprintln(stderr, "Run 'lark --help' for usage.")
	}
	return 2
}

func runCommand(ctx context.Context, args []string, backend hidBackend, out, stderr io.Writer) int {
	return executeCLI(ctx, args, backend, out, stderr, false)
}

func newRootCommand(ctx context.Context, args []string, backend hidBackend, out, stderr io.Writer, route bool) *cobra.Command {
	root := &cobra.Command{
		Use:           "lark",
		Short:         "Control and monitor a Hollyland LARK A2 receiver",
		Version:       version,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")

	run := func(o options) func(*cobra.Command, []string) error {
		return func(_ *cobra.Command, _ []string) error {
			if route && runtime.GOOS == "darwin" && os.Getenv("LARK_DIRECT") != "1" && o.command != "devices" {
				app, err := helperApp()
				if err != nil {
					fmt.Fprintf(stderr, "lark: Lark Helper is unavailable: %v; run scripts/install.sh or set LARK_DIRECT=1 for direct access\n", err)
					return result(4)
				}
				return result(runViaHelper(app, args, out, stderr, o.watch))
			}
			return result(runSelected(ctx, o, backend, out, stderr))
		}
	}

	for _, item := range []struct {
		name, description string
	}{
		{"status", "Show batteries and microphone state"},
		{"info", "Show receiver identity and firmware"},
		{"firmware", "Show receiver firmware"},
		{"devices", "List receiver HID interfaces"},
		{"doctor", "Diagnose receiver access"},
		{"raw", "Show the raw status payload"},
	} {
		o := options{command: item.name}
		root.AddCommand(&cobra.Command{Use: item.name, Short: item.description, Args: cobra.NoArgs, RunE: run(o)})
	}

	battery := &cobra.Command{
		Use:   "battery",
		Short: "Show microphone battery levels",
		Args:  cobra.NoArgs,
	}
	var batteryJSON, watch bool
	var seconds float64
	battery.Flags().BoolVar(&batteryJSON, "json", false, "emit one JSON object per sample")
	battery.Flags().BoolVar(&watch, "watch", false, "poll until Ctrl+C")
	battery.Flags().Float64Var(&seconds, "interval", 5, "watch interval in seconds")
	battery.RunE = func(cmd *cobra.Command, _ []string) error {
		if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 86400 {
			return fmt.Errorf("--interval must be positive and at most 86400 seconds")
		}
		interval := time.Duration(seconds * float64(time.Second))
		if interval < time.Millisecond {
			return fmt.Errorf("--interval must be at least 0.001 seconds")
		}
		return run(options{command: "battery", json: batteryJSON, watch: watch, interval: interval})(cmd, nil)
	}
	root.AddCommand(battery)

	nc := &cobra.Command{
		Use:   "nc",
		Short: "Show or change noise cancellation",
		Long:  "Show noise cancellation by default, or change it with a subcommand.",
		Args:  cobra.NoArgs,
		RunE:  run(options{command: "nc", action: "status"}),
	}
	for _, action := range []struct{ name, description string }{
		{"status", "Show the current state"},
		{"on", "Turn noise cancellation on"},
		{"off", "Turn noise cancellation off"},
	} {
		nc.AddCommand(&cobra.Command{
			Use: action.name, Short: action.description, Args: cobra.NoArgs,
			RunE: run(options{command: "nc", action: action.name}),
		})
	}
	level := &cobra.Command{
		Use:   "level 1|2|3",
		Short: "Set the noise cancellation level",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			value, err := strconv.Atoi(args[0])
			if err != nil || value < 1 || value > 3 {
				return fmt.Errorf("nc level must be 1, 2, or 3")
			}
			return run(options{command: "nc", action: "level", level: value})(cmd, nil)
		},
	}
	nc.AddCommand(level)
	root.AddCommand(nc)

	for _, item := range []struct{ name, description string }{
		{"mute", "Show microphone mute state"},
		{"charging", "Show microphone charging state"},
	} {
		state := &cobra.Command{
			Use: item.name, Short: item.description, Args: cobra.NoArgs,
			RunE: run(options{command: item.name, action: "status"}),
		}
		state.AddCommand(&cobra.Command{
			Use: "status", Short: item.description, Args: cobra.NoArgs,
			RunE: run(options{command: item.name, action: "status"}),
		})
		root.AddCommand(state)
	}
	return root
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--helper-request" {
		os.Exit(runHelper(os.Args[2]))
	}
	os.Exit(executeCLI(context.Background(), os.Args[1:], nativeHID{}, os.Stdout, os.Stderr, true))
}
