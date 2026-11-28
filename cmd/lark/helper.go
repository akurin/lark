package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type helperRequest struct {
	Argv []string `json:"argv"`
}
type helperResult struct {
	ExitCode int `json:"exit_code"`
}

func helperApp() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("helper requires macOS")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, "Applications", "Lark Helper.app", "Contents", "MacOS", "LarkHelper")
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&0111 == 0 {
		return "", fmt.Errorf("helper is not executable")
	}
	marker, err := os.ReadFile(filepath.Join(home, "Applications", "Lark Helper.app", "Contents", "Resources", "lark-build-id"))
	if err != nil || !strings.HasPrefix(string(marker), "go:") {
		return "", fmt.Errorf("Go helper is not installed")
	}
	return path, nil
}

func xmlValue(value string) string {
	var output bytes.Buffer
	_ = xml.EscapeText(&output, []byte(value))
	return output.String()
}

func launchctl(timeout time.Duration, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("launchctl %s: %s: %w", args[0], strings.TrimSpace(string(output)), err)
	}
	return nil
}

func drainFile(path string, offset *int64, out io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(*offset, io.SeekStart); err != nil {
		return err
	}
	n, err := io.Copy(out, file)
	*offset += n
	return err
}

func runViaHelper(app string, args []string, out, stderr io.Writer, watch bool) int {
	directory, err := os.MkdirTemp("", "lark-helper-")
	if err != nil {
		fmt.Fprintf(stderr, "lark: could not create helper request: %v\n", err)
		return 4
	}
	defer os.RemoveAll(directory)
	request, _ := json.Marshal(helperRequest{args})
	if err := os.WriteFile(filepath.Join(directory, "request.json"), request, 0600); err != nil {
		fmt.Fprintf(stderr, "lark: %v\n", err)
		return 4
	}
	for _, name := range []string{"stdout", "stderr"} {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0600); err != nil {
			fmt.Fprintf(stderr, "lark: %v\n", err)
			return 4
		}
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		fmt.Fprintf(stderr, "lark: %v\n", err)
		return 4
	}
	label := "com.akurin.lark.helper.request." + hex.EncodeToString(random)
	domain := "gui/" + strconv.Itoa(os.Getuid())
	target := domain + "/" + label
	plist := `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + xmlValue(label) + `</string><key>ProgramArguments</key><array><string>` + xmlValue(app) + `</string><string>` + xmlValue("--helper-request") + `</string><string>` + xmlValue(directory) + `</string></array><key>RunAtLoad</key><false/><key>KeepAlive</key><false/></dict></plist>`
	if err := os.WriteFile(filepath.Join(directory, "agent.plist"), []byte(plist), 0600); err != nil {
		fmt.Fprintf(stderr, "lark: %v\n", err)
		return 4
	}
	if err := launchctl(15*time.Second, "bootstrap", domain, filepath.Join(directory, "agent.plist")); err != nil {
		fmt.Fprintf(stderr, "lark: could not register Lark Helper: %v\n", err)
		return 4
	}
	defer func() { _ = launchctl(15*time.Second, "bootout", target) }()
	if err := launchctl(15*time.Second, "kickstart", target); err != nil {
		fmt.Fprintf(stderr, "lark: could not start Lark Helper: %v\n", err)
		return 4
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	started := time.Now()
	readyAt := time.Time{}
	var stdoutOffset, stderrOffset int64
	for {
		if err := drainFile(filepath.Join(directory, "stdout"), &stdoutOffset, out); err != nil {
			fmt.Fprintf(stderr, "lark: helper output: %v\n", err)
			return 4
		}
		if err := drainFile(filepath.Join(directory, "stderr"), &stderrOffset, stderr); err != nil {
			fmt.Fprintf(stderr, "lark: helper output: %v\n", err)
			return 4
		}
		data, err := os.ReadFile(filepath.Join(directory, "result.json"))
		if err == nil {
			var result helperResult
			if json.Unmarshal(data, &result) != nil {
				fmt.Fprintln(stderr, "lark: invalid helper result")
				return 5
			}
			return result.ExitCode
		}
		if !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "lark: helper result: %v\n", err)
			return 4
		}
		if readyAt.IsZero() {
			if _, err := os.Stat(filepath.Join(directory, "ready")); err == nil {
				readyAt = time.Now()
			}
		}
		if readyAt.IsZero() && time.Since(started) > 15*time.Second {
			fmt.Fprintln(stderr, "lark: Lark Helper did not start")
			return 4
		}
		if !readyAt.IsZero() && !watch && time.Since(readyAt) > 120*time.Second {
			_ = os.WriteFile(filepath.Join(directory, "cancel"), nil, 0600)
			fmt.Fprintln(stderr, "lark: Lark Helper timed out")
			return 6
		}
		select {
		case <-interrupt:
			_ = os.WriteFile(filepath.Join(directory, "cancel"), nil, 0600)
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(filepath.Join(directory, "result.json")); err == nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			fmt.Fprintln(stderr)
			return 130
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func runHelper(directory string) (code int) {
	code = 4
	resultPath := filepath.Join(directory, "result.json")
	defer func() {
		data, _ := json.Marshal(helperResult{code})
		temporary := resultPath + ".tmp"
		if os.WriteFile(temporary, data, 0600) == nil {
			_ = os.Rename(temporary, resultPath)
		}
	}()
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return code
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return code
	}
	stdout, err := os.OpenFile(filepath.Join(directory, "stdout"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return code
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(filepath.Join(directory, "stderr"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return code
	}
	defer stderr.Close()
	data, err := os.ReadFile(filepath.Join(directory, "request.json"))
	if err != nil {
		fmt.Fprintf(stderr, "lark: Lark Helper: %v\n", err)
		return code
	}
	var request helperRequest
	if err := json.Unmarshal(data, &request); err != nil {
		fmt.Fprintf(stderr, "lark: Lark Helper: %v\n", err)
		return code
	}
	if len(request.Argv) == 0 || len(request.Argv) > 16 || request.Argv[0] == "--helper-request" || request.Argv[0] == "devices" {
		fmt.Fprintln(stderr, "lark: Lark Helper: unsupported command")
		return code
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			if _, err := os.Stat(filepath.Join(directory, "cancel")); err == nil {
				cancel()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()
	_ = os.WriteFile(filepath.Join(directory, "ready"), nil, 0600)
	return runCommand(ctx, request.Argv, nativeHID{}, stdout, stderr)
}
