package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"testing"
	"time"
)

func TestWriteConfigToTorrc(t *testing.T) {

	bridgeLine := "1.2.3.4:1234"
	dataDir := "/foo"
	fileBuf := new(bytes.Buffer)
	torrc := `UseBridges 1
ControlPort unix:/foo/control-socket
SocksPort auto
SafeLogging 0
__DisablePredictedCircuits
DataDirectory /foo
ClientTransportPlugin obfs4 exec /usr/bin/obfs4proxy
PathsNeededToBuildCircuits 0.25
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

func TestCacheFunctions(t *testing.T) {

	cache := make(TestCache)
	bridgeLine := "obfs4 127.0.0.1:1 cert=foo iat-mode=0"

	e := cache.IsCached(bridgeLine)
	if e != nil {
		t.Errorf("Cache is empty but marks bridge line as existing.")
	}

	cache.AddEntry(bridgeLine, nil)
	e = cache.IsCached(bridgeLine)
	if e == nil {
		t.Errorf("Could not retrieve existing element from cache.")
	}

	testError := fmt.Errorf("bridge is on fire")
	cache.AddEntry(bridgeLine, testError)
	e = cache.IsCached(bridgeLine)
	if e.Error != testError.Error() {
		t.Errorf("Got test result %q but expected %q.", e.Error, testError)
	}
}

func TestCacheExpiration(t *testing.T) {

	cache := make(TestCache)

	const shortForm = "2006-Jan-02"
	expiry, _ := time.Parse(shortForm, "2000-Jan-01")
	bridgeLine1 := "1.1.1.1:1111"
	addrPort, _ := BridgeLineToAddrPort(bridgeLine1)
	cache[addrPort] = &CacheEntry{"", expiry}

	bridgeLine2 := "2.2.2.2:2222"
	addrPort, _ = BridgeLineToAddrPort(bridgeLine2)
	cache[addrPort] = &CacheEntry{"", time.Now()}

	e := cache.IsCached(bridgeLine1)
	if e != nil {
		t.Errorf("Expired cache entry was not successfully pruned.")
	}

	e = cache.IsCached(bridgeLine2)
	if e == nil {
		t.Errorf("Valid cache entry was incorrectly pruned.")
	}
}

func BenchmarkIsCached(b *testing.B) {

	getRandAddrPort := func() string {
		return fmt.Sprintf("%d.%d.%d.%d:%d",
			rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(65536))
	}
	getRandError := func() error {
		errors := []error{nil, errors.New("censorship"), errors.New("no censorship")}
		return errors[rand.Intn(len(errors))]
	}

	numCacheEntries := 10000
	cache := make(TestCache)
	for i := 0; i < numCacheEntries; i++ {
		cache.AddEntry(getRandAddrPort(), getRandError())
	}

	// How long does it take to iterate over numCacheEntries cache entries?
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.IsCached("invalid bridge line")
	}
}

func TestCacheSerialisation(t *testing.T) {

	cache := make(TestCache)
	testError := fmt.Errorf("foo")
	cache.AddEntry("1.1.1.1:1", testError)
	cache.AddEntry("2.2.2.2:2", fmt.Errorf("bar"))

	tmpFh, err := ioutil.TempFile(os.TempDir(), "cache-file-")
	if err != nil {
		t.Errorf("Could not create temporary file for test: %s", err)
	}
	defer os.Remove(tmpFh.Name())

	err = cache.WriteToDisk(tmpFh.Name())
	if err != nil {
		t.Errorf("Failed to write cache to disk: %s", err)
	}
	err = cache.ReadFromDisk(tmpFh.Name())
	if err != nil {
		t.Errorf("Failed to read cache from disk: %s", err)
	}

	if len(cache) != 2 {
		t.Errorf("Cache supposed to contain but two elements but has %d.", len(cache))
	}

	e1 := cache.IsCached("1.1.1.1:1")
	if e1 == nil {
		t.Errorf("Cache element supposed to exist but doesn't.")
	}
	if e1.Error != testError.Error() {
		t.Errorf("Error string expected to be %q but is %q.", testError, e1.Error)
	}
}

func TestCacheConcurrency(t *testing.T) {

	cache := make(TestCache)
	max := 10000
	doneReading := make(chan bool)
	doneWriting := make(chan bool)

	// Trigger many concurrent reads and writes, to verify that there are no
	// synchronisation issues.
	go func() {
		for i := 0; i < max; i++ {
			ipAddr := net.IPv4(byte((i>>24)&0xff),
				byte((i>>16)&0xff),
				byte((i>>8)&0xff),
				byte(i&0xff))
			cache.AddEntry(fmt.Sprintf("%s:1234", ipAddr.String()), nil)
		}
		doneWriting <- true
	}()

	go func() {
		for i := 0; i < max; i++ {
			ipAddr := net.IPv4(byte((i>>24)&0xff),
				byte((i>>16)&0xff),
				byte((i>>8)&0xff),
				byte(i&0xff))
			cache.IsCached(fmt.Sprintf("%s:1234", ipAddr.String()))
		}
		doneReading <- true
	}()

	<-doneReading
	<-doneWriting
}

func TestBridgeLineToAddrPort(t *testing.T) {

	_, err := BridgeLineToAddrPort("foo")
	if err == nil {
		t.Errorf("Failed to return error for invalid bridge line.")
	}

	_, err = BridgeLineToAddrPort("obfs4 1.1.1.1 FINGERPRINT")
	if err == nil {
		t.Errorf("Failed to return error for invalid bridge line.")
	}

	addrPort, err := BridgeLineToAddrPort("1.1.1.1:1")
	if err != nil {
		t.Errorf("Failed to accept valid bridge line.")
	}
	if addrPort != "1.1.1.1:1" {
		t.Errorf("Returned invalid addr:port tuple.")
	}

	_, err = BridgeLineToAddrPort("255.255.255.255:12345")
	if err != nil {
		t.Errorf("Failed to accept valid bridge line.")
	}

	_, err = BridgeLineToAddrPort("255.255.255.255:12345 FINGERPRINT")
	if err != nil {
		t.Errorf("Failed to accept valid bridge line.")
	}

	_, err = BridgeLineToAddrPort("obfs4 255.255.255.255:12345 FINGERPRINT")
	if err != nil {
		t.Errorf("Failed to accept valid bridge line.")
	}
}
