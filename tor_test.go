package main

import (
	"bytes"
	"testing"
	"time"
)

func TestWriteConfigToTorrc(t *testing.T) {

	dataDir := "/foo"
	fileBuf := new(bytes.Buffer)
	torrc := `UseBridges 1
ControlPort unix:/foo/control-socket
SocksPort auto
SafeLogging 0
Log notice file /foo/tor.log
DataDirectory /foo
ClientTransportPlugin obfs2,obfs3,obfs4,scramblesuit exec /usr/bin/obfs4proxy -enableLogging -logLevel DEBUG
Bridge obfs4 192.95.36.142:443 CDF2E852BF539B82BD10E27E9115A31734E378C2 cert=qUVQ0srL1JI/vO6V6m/24anYXiJD3QP2HgzUKQtQ7GRqqUvs7P+tG43RtAqdhLOALP7DJQ iat-mode=1
Bridge obfs4 193.11.166.194:27015 2D82C2E354D531A68469ADF7F878FA6060C6BACA cert=4TLQPJrTSaDffMK7Nbao6LC7G9OW/NHkUwIdjLSS3KYf0Nv4/nQiiI8dY2TcsQx01NniOg iat-mode=0
Bridge obfs4 37.218.245.14:38224 D9A82D2F9C2F65A18407B1D2B764F130847F8B5D cert=bjRaMrr1BRiAW8IE9U5z27fQaYgOhX1UCmOpg2pFpoMvo6ZgQMzLsaTzzQNTlm7hNcb+Sg iat-mode=0
`
	err := writeConfigToTorrc(fileBuf, dataDir)
	if err != nil {
		t.Errorf("Failed to write config to torrc: %s", err)
	}

	if torrc != fileBuf.String() {
		t.Errorf("Torrc is not as expected.")
	}
}

func TestGetBridgeIdentifier(t *testing.T) {

	bridgeLine := "obfs4 37.218.245.14:38224 D9A82D2F9C2F65A18407B1D2B764F130847F8B5D cert=bjRaMrr1BRiAW8IE9U5z27fQaYgOhX1UCmOpg2pFpoMvo6ZgQMzLsaTzzQNTlm7hNcb+Sg iat-mode=0"
	identifier, err := getBridgeIdentifier(bridgeLine)
	if err != nil || identifier != "$D9A82D2F9C2F65A18407B1D2B764F130847F8B5D" {
		t.Errorf("failed to extract bridge identifier")
	}

	// Let's try again but this time without fingerprint.
	bridgeLine = "obfs4 37.218.245.14:38224 cert=bjRaMrr1BRiAW8IE9U5z27fQaYgOhX1UCmOpg2pFpoMvo6ZgQMzLsaTzzQNTlm7hNcb+Sg iat-mode=0"
	identifier, err = getBridgeIdentifier(bridgeLine)
	if err != nil || identifier != "37.218.245.14:38224" {
		t.Errorf("failed to extract bridge identifier")
	}
}

func TestBridgeTest(t *testing.T) {

	// Taken from:
	// https://gitlab.torproject.org/tpo/anti-censorship/team/-/wikis/Default-Bridges
	defaultBridge1 := "obfs4 192.95.36.142:443 cert=qUVQ0srL1JI/vO6V6m/24anYXiJD3QP2HgzUKQtQ7GRqqUvs7P+tG43RtAqdhLOALP7DJQ iat-mode=1"
	defaultBridge2 := "obfs4 193.11.166.194:27015 2D82C2E354D531A68469ADF7F878FA6060C6BACA cert=4TLQPJrTSaDffMK7Nbao6LC7G9OW/NHkUwIdjLSS3KYf0Nv4/nQiiI8dY2TcsQx01NniOg iat-mode=0"
	bogusBridge := "127.0.0.1:1"

	TorTestTimeout = time.Minute
	torCtx = &TorContext{TorBinary: "tor"}
	if err := torCtx.Start(); err != nil {
		t.Fatalf("Failed to start tor: %s", err)
	}

	resultChan := make(chan *TestResult)
	req := &TestRequest{
		BridgeLines: []string{defaultBridge1, defaultBridge2, bogusBridge},
		resultChan:  resultChan,
	}
	// Submit the test request.
	torCtx.RequestQueue <- req
	// Now wait for the test result.
	result := <-resultChan

	r, _ := result.Bridges[defaultBridge1]
	if !r.Functional {
		t.Errorf("Default bridge #1 deemed non-functional.")
	}
	r, _ = result.Bridges[defaultBridge2]
	if !r.Functional {
		t.Errorf("Default bridge #2 deemed non-functional.")
	}
	r, _ = result.Bridges[bogusBridge]
	if r.Functional {
		t.Errorf("Bogus bridge deemed functional.")
	}

	if err := torCtx.Stop(); err != nil {
		t.Fatalf("Failed to stop tor: %s", err)
	}
}
