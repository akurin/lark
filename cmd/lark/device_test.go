package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	hid "github.com/sstallion/go-hid"
)

type fakeDevice struct {
	packet   []byte
	readErr  error
	writeErr error
	writes   [][]byte
	closed   bool
}

func (d *fakeDevice) Write(p []byte) (int, error) {
	if d.writeErr != nil {
		return 0, d.writeErr
	}
	d.writes = append(d.writes, append([]byte(nil), p...))
	return len(p), nil
}
func (d *fakeDevice) ReadWithTimeout(p []byte, _ time.Duration) (int, error) {
	if d.readErr != nil {
		return 0, d.readErr
	}
	return copy(p, d.packet), nil
}
func (d *fakeDevice) Close() error { d.closed = true; return nil }

type fakeBackend struct {
	entries        []deviceInfo
	dev            *fakeDevice
	enumerateErr   error
	openErr        error
	openFirstCalls int
}

func (b *fakeBackend) Enumerate() ([]deviceInfo, error) { return b.entries, b.enumerateErr }
func (b *fakeBackend) OpenPath(string) (deviceHandle, error) {
	if b.openErr != nil {
		return nil, b.openErr
	}
	return b.dev, nil
}
func (b *fakeBackend) OpenFirst() (deviceHandle, error) {
	b.openFirstCalls++
	if b.openErr != nil {
		return nil, b.openErr
	}
	return b.dev, nil
}

func TestDeviceStatusAndDisconnect(t *testing.T) {
	d := &fakeDevice{packet: responsePacket(capturedPayload, true)}
	b := &fakeBackend{entries: []deviceInfo{{"path", 1, 2}}, dev: d}
	r, err := openReceiver(b)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.status()
	if err != nil || *s.Microphones[0].Battery != 76 {
		t.Fatalf("status=%+v err=%v", s, err)
	}
	if len(d.writes) != 1 || len(d.writes[0]) != 64 {
		t.Fatal("wrong HID write")
	}
	d.readErr = errors.New("device removed")
	if _, err := r.status(); exitCode(err) != 4 || !strings.Contains(err.Error(), "disconnected") {
		t.Fatalf("disconnect: %v", err)
	}
	r.close()
	if !d.closed {
		t.Fatal("device left open")
	}
}

func TestTimeoutAndNotFound(t *testing.T) {
	r := &receiver{&fakeDevice{readErr: hid.ErrTimeout}}
	if _, err := r.status(); exitCode(err) != 6 {
		t.Fatalf("timeout: %v", err)
	}
	if _, err := openReceiver(&fakeBackend{}); exitCode(err) != 2 {
		t.Fatalf("absent: %v", err)
	}
}

func TestOpenFirstFallback(t *testing.T) {
	b := &fakeBackend{entries: []deviceInfo{{"path", 1, 2}}, openErr: errors.New("open failed")}
	_, err := openReceiver(b)
	if exitCode(err) != 4 || b.openFirstCalls != 1 {
		t.Fatalf("fallback: %v %d", err, b.openFirstCalls)
	}
}

func TestDuplicateEnumeration(t *testing.T) {
	b := &fakeBackend{entries: []deviceInfo{{"path", 1, 2}, {"path", 1, 2}, {"", 1, 2}}}
	entries, err := enumerate(b)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
}

func TestCLIOutputAndCodes(t *testing.T) {
	d := &fakeDevice{packet: responsePacket(capturedPayload, false)}
	b := &fakeBackend{entries: []deviceInfo{{"path", 1, 2}}, dev: d}
	var out, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"battery"}, b, &out, &stderr); code != 0 || out.String() != "Mic 1: 76%\nMic 2: Disconnected\n" {
		t.Fatalf("battery %d %q %q", code, out.String(), stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"battery", "--json"}, b, &out, &stderr); code != 0 || !strings.Contains(out.String(), `"battery":76`) {
		t.Fatalf("json %d %q", code, out.String())
	}
	out.Reset()
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"battery", "--interval", "0"}, b, &out, &stderr); code != 2 {
		t.Fatalf("bad interval: %d", code)
	}
	d.packet = []byte{0xbb, 0xdd, 0x10, 0x80, 0, 16, 1}
	out.Reset()
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"battery"}, b, &out, &stderr); code != 5 {
		t.Fatalf("bad response: %d", code)
	}
}
