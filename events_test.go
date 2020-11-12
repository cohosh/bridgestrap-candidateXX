package main

import (
	"testing"
)

func TestExtractFingerprint(t *testing.T) {

	expected := "0123456789ABCDEF0123456789ABCDEF01234567"
	fingerprint, err := extractFingerprint("650 ORCONN $0123456789ABCDEF0123456789ABCDEF01234567~foobar CLOSED REASON=IOERROR ID=326")
	if err != nil || fingerprint != expected {
		t.Errorf("failed to extract fingerprint")
	}

	fingerprint, err = extractFingerprint("650 NEWDESC $0123456789ABCDEF0123456789ABCDEF01234567~foobar")
	if err != nil || fingerprint != expected {
		t.Errorf("failed to extract fingerprint")
	}

	fingerprint, err = extractFingerprint("650 NEWDESC $0123456789ABCDEF0123456789ABCDEF01234567")
	if err != nil || fingerprint != expected {
		t.Errorf("failed to extract fingerprint")
	}

	_, err = extractFingerprint("650 NEWDESC $ThisIsTooShort")
	if err == nil {
		t.Errorf("failed to reject invalid fingerprint")
	}
}

func TestGetFailureDesc(t *testing.T) {

	desc, err := getFailureDesc("650 ORCONN $0123456789ABCDEF0123456789ABCDEF01234567~Unnamed CLOSED REASON=IOERROR ID=266")
	if err != nil || desc != "We got some other IO error on our connection to the OR." {
		t.Errorf("failed to map error code")
	}
}

func TestCalcMatchLength(t *testing.T) {

}

func TestTorEventStateSuccessful(t *testing.T) {

	// Test run for when we start with an address:port tuple.
	s := NewTorEventState("146.57.248.225:22")
	s.Feed("650 ORCONN 146.57.248.225:22 LAUNCHED ID=69")
	if s.State != BridgeStatePending {
		t.Fatalf("state machine in unexpected state")
	}
	s.Feed("650 ORCONN $10A6CD36A537FCE513A322361547444B393989F0 CONNECTED ID=69")
	if s.State != BridgeStatePending {
		t.Fatalf("state machine in unexpected state")
	}
	s.Feed("650 NEWDESC $10A6CD36A537FCE513A322361547444B393989F0~hopperlab")
	if s.State != BridgeStateSuccess {
		t.Fatalf("state machine in unexpected state")
	}

	// Test run for when we start with a fingerprint.
	s = NewTorEventState("$10A6CD36A537FCE513A322361547444B393989F0")
	s.Feed("650 ORCONN $10A6CD36A537FCE513A322361547444B393989F0 LAUNCHED ID=69")
	if s.State != BridgeStatePending {
		t.Fatalf("state machine in unexpected state")
	}
	s.Feed("650 ORCONN $10A6CD36A537FCE513A322361547444B393989F0 CONNECTED ID=69")
	if s.State != BridgeStatePending {
		t.Fatalf("state machine in unexpected state")
	}
	s.Feed("650 NEWDESC $10A6CD36A537FCE513A322361547444B393989F0~hopperlab")
	if s.State != BridgeStateSuccess {
		t.Fatalf("state machine in unexpected state")
	}
}

func TestTorEventStateFail(t *testing.T) {

	s := NewTorEventState("146.57.248.225:22")
	s.Feed("650 ORCONN 146.57.248.225:22 LAUNCHED ID=69")
	if s.State != BridgeStatePending {
		t.Fatalf("state machine in unexpected state")
	}
	s.Feed("650 ORCONN 146.57.248.225:22 FAILED REASON=DONE ID=69")
	if s.State != BridgeStateFailure {
		t.Fatalf("state machine in unexpected state")
	}
}
