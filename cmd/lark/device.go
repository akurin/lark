package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	hid "github.com/sstallion/go-hid"
)

const (
	vendorID uint16 = 0x3547
	productID uint16 = 0x0411
)

type deviceInfo struct {
	Path string
	UsagePage uint16
	Usage uint16
}

type deviceHandle interface {
	Write([]byte) (int, error)
	ReadWithTimeout([]byte, time.Duration) (int, error)
	Close() error
}

type hidBackend interface {
	Enumerate() ([]deviceInfo, error)
	OpenPath(string) (deviceHandle, error)
	OpenFirst() (deviceHandle, error)
}

type nativeHID struct{}

func (nativeHID) Enumerate() ([]deviceInfo, error) {
	var entries []deviceInfo
	err := hid.Enumerate(vendorID, productID, func(info *hid.DeviceInfo) error {
		if info.Path != "" {
			entries = append(entries, deviceInfo{info.Path, info.UsagePage, info.Usage})
		}
		return nil
	})
	return entries, err
}
func (nativeHID) OpenPath(path string) (deviceHandle, error) { return hid.OpenPath(path) }
func (nativeHID) OpenFirst() (deviceHandle, error) { return hid.OpenFirst(vendorID, productID) }

type larkError struct {
	Code int
	Message string
}
func (e *larkError) Error() string { return e.Message }

func classifyHID(err error, connected bool) error {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "permission"), strings.Contains(message, "access denied"), strings.Contains(message, "not permitted"):
		return &larkError{3, "HID access denied; check Input Monitoring for Lark Helper"}
	case strings.Contains(message, "busy"), strings.Contains(message, "exclusive"):
		return &larkError{4, "HID device busy; close HollyAudio or other receiver controllers"}
	case connected:
		return &larkError{4, "receiver disconnected or HID I/O failed: " + err.Error()}
	default:
		return &larkError{4, "could not open HID receiver: " + err.Error()}
	}
}

type receiver struct { dev deviceHandle }

func enumerate(backend hidBackend) ([]deviceInfo, error) {
	entries, err := backend.Enumerate()
	if err != nil { return nil, classifyHID(err, false) }
	seen := map[string]bool{}
	unique := make([]deviceInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.Path != "" && !seen[entry.Path] { unique = append(unique, entry); seen[entry.Path] = true }
	}
	return unique, nil
}

func openReceiver(backend hidBackend) (*receiver, error) {
	entries, err := enumerate(backend)
	if err != nil { return nil, err }
	if len(entries) == 0 { return nil, &larkError{2, "LARK A2 receiver not found (USB 3547:0411); plug in the USB-C receiver"} }
	sort.SliceStable(entries, func(i, j int) bool {
		rank := func(page uint16) int { if page == 0xff90 { return 0 }; if page == 0xff82 { return 1 }; return 2 }
		return rank(entries[i].UsagePage) < rank(entries[j].UsagePage)
	})
	var firstErr error
	for _, entry := range entries {
		dev, openErr := backend.OpenPath(entry.Path)
		if openErr == nil { return &receiver{dev}, nil }
		classified := classifyHID(openErr, false)
		if firstErr == nil || exitCode(classified) == 3 { firstErr = classified }
	}
	if len(entries) == 1 {
		dev, openErr := backend.OpenFirst()
		if openErr == nil { return &receiver{dev}, nil }
		classified := classifyHID(openErr, false)
		if firstErr == nil || exitCode(classified) == 3 { firstErr = classified }
	}
	return nil, firstErr
}

func (r *receiver) close() { if r != nil && r.dev != nil { _ = r.dev.Close() } }

func (r *receiver) send(command byte, payload []byte) error {
	packet, err := makeRequest(command, payload)
	if err != nil { return &larkError{5, err.Error()} }
	n, err := r.dev.Write(packet)
	if err != nil { return classifyHID(err, true) }
	if n != len(packet) { return &larkError{4, fmt.Sprintf("receiver write was incomplete: %d of %d bytes", n, len(packet))} }
	return nil
}

func (r *receiver) exchange(command byte, payload []byte) ([]byte, error) {
	if err := r.send(command, payload); err != nil { return nil, err }
	buffer := make([]byte, 256)
	n, err := r.dev.ReadWithTimeout(buffer, time.Second)
	if errors.Is(err, hid.ErrTimeout) || (err == nil && n == 0) {
		return nil, &larkError{6, "receiver did not answer within 1 second"}
	}
	if err != nil { return nil, classifyHID(err, true) }
	data, err := parseResponse(buffer[:n], command)
	if err != nil { return nil, &larkError{5, err.Error()} }
	return data, nil
}

func (r *receiver) status() (status, error) {
	data, err := r.exchange(statusCommand, nil)
	if err != nil { return status{}, err }
	s, err := decodeStatus(data)
	if err != nil { return status{}, &larkError{5, err.Error()} }
	return s, nil
}

func (r *receiver) firmware() (string, error) {
	data, err := r.exchange(firmwareCommand, nil)
	if err != nil { return "", err }
	version, err := decodeFirmware(data)
	if err != nil { return "", &larkError{5, err.Error()} }
	return version, nil
}

func (d deviceInfo) String() string {
	return fmt.Sprintf("3547:0411 usage_page=0x%04x usage=0x%04x path=%q", d.UsagePage, d.Usage, d.Path)
}
