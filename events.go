package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"regexp"
	"strconv"
)

const (
	// The number of hex digits in a bridge's fingerprint, e.g.:
	// 0123456789ABCDEF0123456789ABCDEF01234567
	BridgeFingerprintLen = 40
	BridgeStatePending   = iota
	BridgeStateSuccess
	BridgeStateFailure
)

// Examples of ORCONN events:
//   650 ORCONN 90.41.70.32:7434 LAUNCHED ID=75
//   650 ORCONN $D9A82D2F9C2F65A18407B1D2B764F130847F8B5D LAUNCHED ID=38
//   650 ORCONN $D9A82D2F9C2F65A18407B1D2B764F130847F8B5D~dragon CLOSED REASON=DONE ID=42
//   650 ORCONN $9695DFC35FFEB861329B9F1AB04C46397020CE31~moria1 CLOSED REASON=IOERROR ID=1833
//   650 ORCONN 128.31.0.33:9101 FAILED REASON=TIMEOUT NCIRCS=1 ID=1836
//   650 ORCONN $D9A82D2F9C2F65A18407B1D2B764F130847F8B5D~dragon CONNECTED ID=42
var OrConnFields = regexp.MustCompile(`ORCONN ([^ ]*) ([^ ]*).*ID=([0-9]*)`)
var OrConnEvent = regexp.MustCompile(`^650 ORCONN`)
var OrConnReasonField = regexp.MustCompile(`^650 ORCONN.*REASON=([A-Z]*)`)
var NewDescEvent = regexp.MustCompile(`^650 NEWDESC`)
var Fingerprint = regexp.MustCompile(`([A-F0-9]{40})`)

// extractFingerprint extracts a bridge's fingerprint from the given ORCONN or
// NEWDESC line.
func extractFingerprint(line string) (string, error) {

	result := string(Fingerprint.Find([]byte(line)))

	if result == "" {
		return result, errors.New("could not extract fingerprint from line")
	} else {
		return result, nil
	}
}

// getFailureDesc takes as input an ORCONN line and maps the error code to a
// more descriptive string.
func getFailureDesc(line string) (string, error) {

	// See the following part of our control specification:
	// https://gitweb.torproject.org/torspec.git/tree/control-spec.txt?id=1ecf3f66586816fc718e38f8cd7cbb23fa9b81f5#n2472
	var reasons = map[string]string{
		"DONE":           "The OR connection has shut down cleanly.",
		"CONNECTREFUSED": "We got an ECONNREFUSED while connecting to the target OR.",
		"IDENTITY":       "We connected to the OR, but found that its identity was not what we expected.",
		"CONNECTRESET":   "We got an ECONNRESET or similar IO error from the connection with the OR.",
		"TIMEOUT":        "We got an ETIMEOUT or similar IO error from the connection with the OR, or we're closing the connection for being idle for too long.",
		"NOROUTE":        "We got an ENOTCONN, ENETUNREACH, ENETDOWN, EHOSTUNREACH, or similar error while connecting to the OR.",
		"IOERROR":        "We got some other IO error on our connection to the OR.",
		"RESOURCELIMIT":  "We don't have enough operating system resources (file descriptors, buffers, etc) to connect to the OR.",
		"PT_MISSING":     "No pluggable transport was available.",
		"MISC":           "The OR connection closed for some other reason.",
	}

	matches := OrConnReasonField.FindStringSubmatch(line)
	expectedMatches := 2
	if len(matches) != expectedMatches {
		return "", fmt.Errorf("expected %d but got %d matches", expectedMatches, len(matches))
	}

	desc, exists := reasons[matches[1]]
	if !exists {
		return "", fmt.Errorf("could not find reason for %q", matches[1])
	}

	return desc, nil
}

// calcMatchLength determines the number of digits that we should compare for
// in an ORCONN LAUNCHED event.
func calcMatchLength(target1, target2 string) int {

	// Start with min(target1, target2).
	length := len(target1)
	if len(target2) < len(target1) {
		length = len(target2)
	}

	// We're dealing with a "$fingerprint~name" pattern.
	if length > BridgeFingerprintLen+1 {
		length = BridgeFingerprintLen + 1
	}

	return length
}

// TorEventState represents a state machine that we use to parse ORCONN and
// NEWDESC events.
type TorEventState struct {
	ConnIds     map[int]bool
	State       int
	Reason      string
	Fingerprint string
	Target      string // If present, the fingerprint; otherwise address:port.
	TestId      int
}

// NewTorEventState returns a new TorEventState struct.
func NewTorEventState(target string) *TorEventState {

	testId := rand.Intn(math.MaxInt32)
	log.Printf("%x: Creating new TorEventState with %s bridge identifier.", testId, target)
	return &TorEventState{ConnIds: make(map[int]bool),
		Target: target,
		TestId: testId,
		State:  BridgeStatePending}
}

// Feed takes as input a new Tor event line.
func (t *TorEventState) Feed(line string) {

	if OrConnEvent.MatchString(line) {
		t.processOrConnLine(line)
	} else if NewDescEvent.MatchString(line) {
		t.processNewDescLine(line)
	} else {
		log.Printf("%x: Bug: Received an unexpected event %q.", t.TestId, line)
	}
}

// processOrConnLine processes ORCONN lines.
func (t *TorEventState) processOrConnLine(line string) {

	matches := OrConnFields.FindStringSubmatch(line)
	if len(matches) != 4 {
		log.Printf("%x: Bug: Unexpected number of substring matches in %q", t.TestId, line)
		return
	}

	target := matches[1]
	eventType := matches[2]
	i, err := strconv.Atoi(matches[3])
	if err != nil {
		log.Printf("%x: Bug: Could not convert %q to integer: %s", t.TestId, matches[2], err)
		return
	}

	// Are we dealing with a new ORCONN for our bridge line?  If so, add its ID
	// to our map, so we can keep track of it.
	if eventType == "LAUNCHED" {
		matchLen := calcMatchLength(target, t.Target)
		if target == t.Target[:matchLen] {
			log.Printf("%x: Adding ID %d to map.", t.TestId, i)
			t.ConnIds[i] = true
		}
	}

	// Are we dealing with an ORCONN for a bridge line that isn't ours?  If so,
	// let's get outta here.
	if _, exists := t.ConnIds[i]; !exists {
		return
	}

	// Now decide what to do.  Here are the event types we're dealing with:
	// https://gitweb.torproject.org/torspec.git/tree/control-spec.txt#n2448
	switch eventType {
	case "FAILED":
		// An ORCONN failed.  Was it ours?
		if _, exists := t.ConnIds[i]; exists {
			log.Printf("%x: Setting ORCONN failure.", t.TestId)
			t.State = BridgeStateFailure
		}

		// Extract the "REASON" field to learn what happened.
		desc, err := getFailureDesc(line)
		if err != nil {
			log.Printf("%x: Bug: %s", t.TestId, err)
		} else {
			log.Printf("%x: ORCONN failed because: %s", t.TestId, desc)
		}
		t.Reason = desc
	case "CONNECTED":
		fingerprint, err := extractFingerprint(line)
		if err == nil {
			log.Printf("%x: Setting fingerprint to %s.", t.TestId, fingerprint)
			t.Fingerprint = fingerprint
		} else {
			log.Printf("%x: Bug: Failed to extract fingerprint from %q.", t.TestId, line)
		}

		// An ORCONN succeeded.  Was it ours?
		if _, exists := t.ConnIds[i]; exists {
			log.Printf("%x: ORCONN success.  One step closer to NEWDESC.", t.TestId)
		}
	}
}

// processNewDescLine processes NEWDESC lines.
func (t *TorEventState) processNewDescLine(line string) {

	// Examples of valid NEWDESC events:
	//   650 NEWDESC $CDF2E852BF539B82BD10E27E9115A31734E378C2~Lisbeth
	//   650 NEWDESC $CDF2E852BF539B82BD10E27E9115A31734E378C2
	fingerprint, err := extractFingerprint(line)
	if err != nil {
		log.Printf("%x: Bug: Could not extract fingerprint from %q.", t.TestId, line)
		return
	}

	// Is the NEWDESC event ours?
	if fingerprint == t.Fingerprint {
		log.Printf("%x: Received NEWDESC event for our bridge.", t.TestId)
		t.State = BridgeStateSuccess
	}
}
