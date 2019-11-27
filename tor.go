package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/yawning/bulb"
)

const (
	// Sixty seconds is a reasonable timeout according to:
	// <https://bugs.torproject.org/32126#comment:1>
	TorBootstrapTimeout = 60 * time.Second
	CacheValidity       = 24 * time.Hour
)

// CacheEntry represents an entry in our cache of bridges that we recently
// tested.  Error is nil if a bridge works, and otherwise holds an error
// string.  Time determines when we tested the bridge.
type CacheEntry struct {
	Error error
	Time  time.Time
}

type TestCache map[string]*CacheEntry

var cache TestCache = make(TestCache)

// Regular expressions that match tor's bootstrap status events.
var success = regexp.MustCompile(`PROGRESS=100`)
var warning = regexp.MustCompile(`STATUS_CLIENT WARN BOOTSTRAP`)

// BridgeLineToAddrPort takes a bridge line as input and returns a string
// consisting of the bridge's addr:port.
func BridgeLineToAddrPort(bridgeLine string) (string, error) {

	// Represents an addr:port tuple.
	re := regexp.MustCompile(`(?:[0-9]{1,3}\.){3}[0-9]{1,3}:[0-9]{1,5}`)
	result := string(re.Find([]byte(bridgeLine)))

	if result == "" {
		return result, fmt.Errorf("could not extract addr:port from bridge line")
	} else {
		return result, nil
	}
}

// IsCached returns a cache entry if the given bridge line has been tested
// recently (as determined by CacheValidity), and nil otherwise.
func (tc *TestCache) IsCached(bridgeLine string) *CacheEntry {

	// First, prune expired cache entries.
	now := time.Now()
	for index, entry := range *tc {
		if entry.Time.Before(now.Add(-CacheValidity)) {
			delete(*tc, index)
		}
	}

	addrPort, err := BridgeLineToAddrPort(bridgeLine)
	if err != nil {
		return nil
	}

	return (*tc)[addrPort]
}

// AddEntry adds an entry for the given bridge and test result to our cache.
func (tc *TestCache) AddEntry(bridgeLine string, result error) {

	addrPort, err := BridgeLineToAddrPort(bridgeLine)
	if err != nil {
		return
	}

	log.Printf("Caching %q: %q", addrPort, result)
	(*tc)[addrPort] = &CacheEntry{result, time.Now()}
}

// getDomainSocketPath takes as input the path to our data directory and
// returns the path to the domain socket for tor's control port.
func getDomainSocketPath(dataDir string) string {
	return fmt.Sprintf("%s/control-socket", dataDir)
}

// writeConfigToTorrc writes the content of a Tor config file (including the
// given bridgeLine and dataDir) to the given file handle.
func writeConfigToTorrc(tmpFh io.Writer, dataDir, bridgeLine string) error {

	_, err := fmt.Fprintf(tmpFh, "UseBridges 1\n"+
		"ControlPort unix:%s\n"+
		"SocksPort auto\n"+
		"SafeLogging 0\n"+
		"__DisablePredictedCircuits\n"+
		"DataDirectory %s\n"+
		"ClientTransportPlugin obfs4 exec /usr/bin/obfs4proxy\n"+
		"PathsNeededToBuildCircuits 0.25\n"+
		"Bridge %s", getDomainSocketPath(dataDir), dataDir, bridgeLine)

	return err
}

// makeControlConnection attempts to establish a control connection with Tor's
// given domain socket.  If successful, it returns the connection.  Otherwise,
// it returns an error.
func makeControlConnection(domainSocket string) (*bulb.Conn, error) {

	var torCtrl *bulb.Conn
	var err error

	// Try connecting to tor's control socket.  It may take a second or two for
	// it to be ready.
	for attempts := 0; attempts < 10; attempts++ {
		torCtrl, err = bulb.Dial("unix", domainSocket)
		if err == nil {
			if err := torCtrl.Authenticate(""); err != nil {
				return nil, fmt.Errorf("authentication with tor's control port failed: %v", err)
			}
			return torCtrl, nil
		} else {
			time.Sleep(1 * time.Second)
		}
	}

	return nil, fmt.Errorf("unable to connect to domain socket")
}

// bootstrapTorOverBridge implements a cache around
// bootstrapTorOverBridgeWrapped.
func bootstrapTorOverBridge(bridgeLine string) error {
	if cacheEntry := cache.IsCached(bridgeLine); cacheEntry != nil {
		return cacheEntry.Error
	}

	err := bootstrapTorOverBridgeWrapped(bridgeLine)
	cache.AddEntry(bridgeLine, err)

	return err
}

// bootstrapTorOverBridgeWrapped attempts to bootstrap a Tor connection over
// the given bridge line.  This function returns nil if the bootstrap succeeds
// and an error otherwise.
func bootstrapTorOverBridgeWrapped(bridgeLine string) error {

	// Create our torrc.
	tmpFh, err := ioutil.TempFile(os.TempDir(), "torrc-")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFh.Name())

	// Create our data directory.
	tmpDir, err := ioutil.TempDir(os.TempDir(), "tor-datadir-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err = writeConfigToTorrc(tmpFh, tmpDir, bridgeLine); err != nil {
		return err
	}

	// Terminate our process after one minute.
	ctx, cancel := context.WithTimeout(context.Background(), TorBootstrapTimeout)
	defer cancel()

	log.Printf("Testing bridge line %q.", bridgeLine)
	// Start tor but don't wait for the process to complete, so our call
	// returns right away.
	cmd := exec.CommandContext(ctx, "tor", "-f", tmpFh.Name())
	if err = cmd.Start(); err != nil {
		return err
	}

	torCtrl, err := makeControlConnection(getDomainSocketPath(tmpDir))
	if err != nil {
		return err
	}
	defer torCtrl.Close()

	// Start our async reader and listen for STATUS_CLIENT events, which
	// include bootstrap messages:
	// <https://gitweb.torproject.org/torspec.git/tree/control-spec.txt?id=b7cfa8619947be4a377366365f5ddee8e0733330#n2499>
	torCtrl.StartAsyncReader()
	if _, err := torCtrl.Request("SETEVENTS STATUS_CLIENT"); err != nil {
		return fmt.Errorf("command SETEVENTS STATUS_CLIENT failed: %v", err)
	}

	// Keep reading events until one of the following happens:
	// 1) tor bootstrapped to 100%
	// 2) tor encountered a warning while bootstrapping
	// 3) we hit our timeout, which interrupts our call to NextEvent()
	for {
		ev, err := torCtrl.NextEvent()
		if err != nil {
			return err
		}
		log.Printf("Controller: %s", ev.RawLines)
		for _, line := range ev.RawLines {
			if success.MatchString(line) {
				return nil
			} else if warning.MatchString(line) {
				re := regexp.MustCompile(`WARNING="([^"]*)"`)
				matches := re.FindStringSubmatch(line)
				if len(matches) != 2 {
					log.Printf("Unexpected number of substring matches: %q", matches)
					return fmt.Errorf("could not bootstrap")
				}
				return fmt.Errorf(matches[1])
			}
		}
	}

	return fmt.Errorf("could not bootstrap")
}
