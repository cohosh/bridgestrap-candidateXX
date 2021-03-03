package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yawning/bulb"
)

const (
	// We're using the following default bridges to bootstrap our Tor instance.
	// Once it's bootstrapped, we no longer need them.
	DefaultBridge1 = "obfs4 192.95.36.142:443 CDF2E852BF539B82BD10E27E9115A31734E378C2 cert=qUVQ0srL1JI/vO6V6m/24anYXiJD3QP2HgzUKQtQ7GRqqUvs7P+tG43RtAqdhLOALP7DJQ iat-mode=1"
	DefaultBridge2 = "obfs4 193.11.166.194:27015 2D82C2E354D531A68469ADF7F878FA6060C6BACA cert=4TLQPJrTSaDffMK7Nbao6LC7G9OW/NHkUwIdjLSS3KYf0Nv4/nQiiI8dY2TcsQx01NniOg iat-mode=0"
	DefaultBridge3 = "obfs4 37.218.245.14:38224 D9A82D2F9C2F65A18407B1D2B764F130847F8B5D cert=bjRaMrr1BRiAW8IE9U5z27fQaYgOhX1UCmOpg2pFpoMvo6ZgQMzLsaTzzQNTlm7hNcb+Sg iat-mode=0"
	// The maximum amount of bridges per batch.
	MaxBridgesPerReq  = 100
	MaxEventBacklog   = 100
	MaxRequestBacklog = 100
)

// The amount of time we give Tor to test a batch of bridges.
var TorTestTimeout time.Duration

// getBridgeIdentifier turns the given bridgeLine into a canonical identifier
// that we use to look for relevant ORCONN events.  If the given bridge line
// contains a fingerprint, the function returns $FINGERPRINT.  If it doesn't,
// the function returns the address:port tuple of the given bridge line.
func getBridgeIdentifier(bridgeLine string) (string, error) {

	re := regexp.MustCompile(`([A-F0-9]{40})`)
	if result := string(re.Find([]byte(bridgeLine))); result != "" {
		return "$" + result, nil
	}

	if result := string(AddrPortBridgeLine.Find([]byte(bridgeLine))); result != "" {
		return result, nil
	}

	return "", errors.New("could not extract bridge identifier")
}

// getDomainSocketPath takes as input the path to our data directory and
// returns the path to the domain socket for tor's control port.
func getDomainSocketPath(dataDir string) string {
	return fmt.Sprintf("%s/control-socket", dataDir)
}

// writeConfigToTorrc writes a Tor config file to the given file handle.
func writeConfigToTorrc(tmpFh io.Writer, dataDir string) error {

	_, err := fmt.Fprintf(tmpFh, "UseBridges 1\n"+
		"ControlPort unix:%s\n"+
		"SocksPort auto\n"+
		"SafeLogging 0\n"+
		"Log notice file %s/tor.log\n"+
		"DataDirectory %s\n"+
		"ClientTransportPlugin obfs2,obfs3,obfs4,scramblesuit exec /usr/bin/obfs4proxy -enableLogging -logLevel DEBUG\n"+
		"Bridge %s\n"+
		"Bridge %s\n"+
		"Bridge %s\n", getDomainSocketPath(dataDir), dataDir, dataDir,
		DefaultBridge1, DefaultBridge2, DefaultBridge3)

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
			torCtrl.Debug(true)
			if err := torCtrl.Authenticate(""); err != nil {
				return nil, fmt.Errorf("authentication with tor's control port failed: %v", err)
			}
			return torCtrl, nil
		} else {
			time.Sleep(time.Second)
		}
	}

	return nil, fmt.Errorf("unable to connect to domain socket")
}

// TorContext represents the data structures and methods we need to control a
// Tor process.
type TorContext struct {
	sync.Mutex
	Ctrl         *bulb.Conn
	DataDir      string
	Cancel       context.CancelFunc
	Context      context.Context
	RequestQueue chan *TestRequest
	TorBinary    string
	eventChan    chan *bulb.Response
	shutdown     chan bool
}

// Stop stops the Tor process.  Errors during cleanup are logged and the last
// occuring error is returned.
func (c *TorContext) Stop() error {
	c.Lock()
	defer c.Unlock()

	var err error
	close(c.shutdown)
	log.Println("Stopping Tor process.")
	c.Cancel()

	if c.Ctrl != nil {
		if err = c.Ctrl.Close(); err != nil {
			log.Printf("Failed to close control connection: %s", err)
		}
	}

	if err = os.RemoveAll(c.DataDir); err != nil {
		log.Printf("Failed to remove data directory: %s", err)
	}
	return err
}

// Start starts the Tor process.
func (c *TorContext) Start() error {
	c.Lock()
	defer c.Unlock()
	log.Println("Starting Tor process.")

	c.eventChan = make(chan *bulb.Response, MaxEventBacklog)
	c.RequestQueue = make(chan *TestRequest, MaxRequestBacklog)
	c.shutdown = make(chan bool)

	// Create Tor's data directory.
	var err error
	c.DataDir, err = ioutil.TempDir(os.TempDir(), "tor-datadir-")
	if err != nil {
		return err
	}
	log.Printf("Created data directory %q.", c.DataDir)

	// Create our torrc.
	tmpFh, err := ioutil.TempFile(c.DataDir, "torrc-")
	if err != nil {
		return err
	}
	if err = writeConfigToTorrc(tmpFh, c.DataDir); err != nil {
		return err
	}
	log.Println("Wrote Tor config file.")

	// Start our Tor process.
	c.Context, c.Cancel = context.WithCancel(context.Background())
	cmd := exec.CommandContext(c.Context, c.TorBinary, "-f", tmpFh.Name())
	if err = cmd.Start(); err != nil {
		return err
	}
	log.Println("Started Tor process.")

	// Start a control connection with our Tor process.
	c.Ctrl, err = makeControlConnection(getDomainSocketPath(c.DataDir))
	if err != nil {
		return nil
	}
	c.Ctrl.StartAsyncReader()
	go c.eventReader()
	go c.dispatcher()

	if _, err := c.Ctrl.Request("SETEVENTS ORCONN NEWDESC"); err != nil {
		return err
	}

	return nil
}

// TestBridgeLines takes as input a list of bridge lines, tells Tor to test
// them, and returns the resulting TestResult.
func (c *TorContext) TestBridgeLines(bridgeLines []string) *TestResult {
	c.Lock()
	defer c.Unlock()

	if len(bridgeLines) == 0 {
		return NewTestResult()
	}

	result := NewTestResult()
	log.Printf("Testing %d bridge lines.", len(bridgeLines))

	// We maintain per-bridge state machines that parse Tor's event output.
	eventParsers := make(map[string]*TorEventState)
	for _, bridgeLine := range bridgeLines {
		identifier, err := getBridgeIdentifier(bridgeLine)
		if err != nil {
			log.Printf("Bug: Could not extract identifier from bridge line %q.", bridgeLine)
			continue
		}
		eventParsers[bridgeLine] = NewTorEventState(identifier)
	}

	// By default, Tor enters dormant mode 24 hours after seeing no user
	// activity.  Bridgestrap's control port interaction doesn't count as user
	// activity, which is why we explicitly wake up Tor before issuing our
	// SETCONF.  See the following issue for more details:
	// https://gitlab.torproject.org/tpo/anti-censorship/bridgestrap/-/issues/12
	if _, err := c.Ctrl.Request("SIGNAL ACTIVE"); err != nil {
		log.Printf("Bug: error after sending SIGNAL ACTIVE: %s", err)
		result.Error = err.Error()
		return result
	}

	// Create our SETCONF line, which tells Tor what bridges it should test.
	// It has the following format:
	//   SETCONF Bridge="BRIDGE1" Bridge="BRIDGE2" ...
	cmdPieces := []string{"SETCONF"}
	for _, bridgeLine := range bridgeLines {
		cmdPieces = append(cmdPieces, fmt.Sprintf("Bridge=%q", bridgeLine))
	}
	cmd := strings.Join(cmdPieces, " ")

	if _, err := c.Ctrl.Request(cmd); err != nil {
		result.Error = err.Error()
		return result
	}

	log.Printf("Waiting for Tor to give us test results.")
	timeout := time.After(TorTestTimeout)
	for {
		select {
		case ev := <-c.eventChan:
			// Our channel is closed.
			if ev == nil {
				result.Error = "test aborted because bridgestrap is shutting down"
				return result
			}
			for _, line := range ev.RawLines {
				for bridgeLine, parser := range eventParsers {
					// Skip bridges that are done testing.
					if parser.State != BridgeStatePending {
						continue
					}
					parser.Feed(line)
					if parser.State == BridgeStateSuccess {
						log.Printf("Setting %s to 'true'", bridgeLine)
						result.Bridges[bridgeLine] = &BridgeTest{
							Functional: true,
							LastTested: time.Now().UTC(),
						}
					} else if parser.State == BridgeStateFailure {
						log.Printf("Setting %s to 'false'", bridgeLine)
						result.Bridges[bridgeLine] = &BridgeTest{
							Functional: false,
							Error:      parser.Reason,
							LastTested: time.Now().UTC(),
						}
					}
				}

				// Do we have test results for all bridges?  If so, we're done.
				if len(result.Bridges) == len(bridgeLines) {
					return result
				}
			}
		case <-timeout:
			log.Printf("Tor process timed out.")

			// Mark whatever bridge results we're missing as nonfunctional.
			for _, bridgeLine := range bridgeLines {
				if _, exists := result.Bridges[bridgeLine]; !exists {
					result.Bridges[bridgeLine] = &BridgeTest{
						Functional: false,
						Error:      "timed out waiting for bridge descriptor",
						LastTested: time.Now().UTC(),
					}
				}
			}
			return result
		}
	}

	return result
}

// dispatcher reads new bridge test requests, triggers the test, and writes the
// result to the given channel.
func (c *TorContext) dispatcher() {
	log.Printf("Starting request dispatcher.")
	defer log.Printf("Stopping request dispatcher.")
	for {
		select {
		case req := <-c.RequestQueue:
			log.Printf("%d pending test requests.", len(c.RequestQueue))
			metrics.PendingReqs.Set(float64(len(c.RequestQueue)))

			start := time.Now()
			result := c.TestBridgeLines(req.BridgeLines)
			elapsed := time.Since(start)
			metrics.TorTestTime.Observe(elapsed.Seconds())

			req.resultChan <- result
		case <-c.eventChan:
			// Discard events that happen while we are not testing bridges.
			log.Printf("Discarding event because we're not testing bridges.")
		case <-c.shutdown:
			return
		}
	}
}

// eventReader reads events from Tor's control port and writes them to
// c.eventChan, allowing TestBridgeLines to read Tor's events in a select
// statement.
func (c *TorContext) eventReader() {
	log.Println("Starting event reader.")
	defer log.Printf("Stopping event reader.")
	for {
		ev, err := c.Ctrl.NextEvent()
		if err != nil {
			close(c.eventChan)
			return
		}
		metrics.PendingEvents.Set(float64(len(c.eventChan)))
		c.eventChan <- ev
	}
}
