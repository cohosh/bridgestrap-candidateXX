package main

import (
	"bytes"
	"testing"
)

func TestTorHasBootstrapped(t *testing.T) {

	r := torHasBootstrapped(`Oct 29 10:08:31.000 [notice] Bootstrapped 100%: Done`)
	if !r {
		t.Errorf("torHasBootstrapped failed to realise that Tor has bootstrapped.")
	}

	r = torHasBootstrapped(`Oct 29 10:08:30.000 [notice] Bootstrapped 90%: Establishing a Tor circuit`)
	if r {
		t.Errorf("torHasBootstrapped failed to realise that Tor has not bootstrapped.")
	}
}

func TestTorEncounteredError(t *testing.T) {

	r := torEncounteredError(`Oct 29 10:15:33.000 [warn] Proxy Client: unable to connect to 3.135.154.16:41609 ("general SOCKS server failure")`)
	if !r {
		t.Errorf("torEncounteredError failed to recognise bootstrapping error.")
	}

	r = torEncounteredError(`Oct 29 10:08:31.000 [notice] Bootstrapped 100%: Done`)
	if r {
		t.Errorf("torEncounteredError incorrectly detected a bootstrapping error.")
	}

	r = torEncounteredError(`Oct 29 10:17:49.000 [warn] Problem bootstrapping. Stuck at 5%: Connecting to directory server. (Can't connect to bridge; PT_MISSING; count 4; recommendation warn; host 0000000000000000000000000000000000000000 at 1.2.3.4:1234)`)
	if !r {
		t.Errorf("torEncounteredError failed to recognise bootstrapping error.")
	}
}

func TestWriteConfigToTorrc(t *testing.T) {

	bridgeLine := "1.2.3.4:1234"
	dataDir := "/foo"
	fileBuf := new(bytes.Buffer)
	torrc := `UseBridges 1
SocksPort auto
DataDirectory /foo
ClientTransportPlugin obfs4 exec /usr/bin/obfs4proxy
Bridge 1.2.3.4:1234`

	err := writeConfigToTorrc(fileBuf, dataDir, bridgeLine)
	if err != nil {
		t.Errorf("Failed to write config to torrc: %s", err)
	}

	if torrc != fileBuf.String() {
		t.Errorf("Torrc is not as expected.")
	}
}

func TestBootstrapTorOverBridge(t *testing.T) {

	// This default bridge is online as of 2019-10-29:
	// <https://trac.torproject.org/projects/tor/wiki/doc/TorBrowser/DefaultBridges>
	defaultBridge := "obfs4 193.11.166.194:27015 cert=4TLQPJrTSaDffMK7Nbao6LC7G9OW/NHkUwIdjLSS3KYf0Nv4/nQiiI8dY2TcsQx01NniOg iat-mode=0"
	err := bootstrapTorOverBridge(defaultBridge)
	if err != nil {
		t.Errorf("Failed to label default bridge as available: %s", err)
	}

	brokenBridge := "obfs1 foo"
	err = bootstrapTorOverBridge(brokenBridge)
	if err == nil {
		t.Errorf("Failed to label default bridge as broken.")
	}

	brokenBridge = "obfs4 127.0.0.1:1 cert=foo iat-mode=0"
	err = bootstrapTorOverBridge(brokenBridge)
	if err == nil {
		t.Errorf("Failed to label default bridge as broken.")
	}
}
