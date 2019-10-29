package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"time"
)

const (
	// Sixty seconds is a reasonable timeout according to:
	// <https://bugs.torproject.org/32126#comment:1>
	TorBootstrapTimeout = 60 * time.Second
)

// torHasBootstrapped returns true if the given log line indicates that tor has
// successfully bootstrapped and false otherwise.
func torHasBootstrapped(line string) bool {

	re := regexp.MustCompile(`Bootstrapped 100%`)
	return re.Match([]byte(line))
}

// torEncounteredError returns true if the given log line indicates that tor
// encountered an error while bootstrapping and false otherwise.
func torEncounteredError(line string) bool {

	// FIXME: Find a better way to handle this.
	re := regexp.MustCompile(`(Problem bootstrapping|Bridge line|unable to connect)`)
	return re.Match([]byte(line))
}

// writeConfigToTorrc writes the content of a Tor config file (including the
// given bridgeLine and dataDir) to the given file handle.
func writeConfigToTorrc(tmpFh io.Writer, dataDir, bridgeLine string) error {

	// FIXME: Optimise our configuration file.
	_, err := fmt.Fprintf(tmpFh, "UseBridges 1\n"+
		"SocksPort auto\n"+
		"DataDirectory %s\n"+
		"ClientTransportPlugin obfs4 exec /usr/bin/obfs4proxy\n"+
		"Bridge %s", dataDir, bridgeLine)

	return err
}

// bootstrapTorOverBridge attempts to bootstrap a Tor connection over the given
// bridge line.  This function returns nil if the bootstrap succeeds and an
// error otherwise.
func bootstrapTorOverBridge(bridgeLine string) error {

	log.Printf("Creating temporary torrc file.")

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

	writeConfigToTorrc(tmpFh, tmpDir, bridgeLine)

	// Terminate our process after one minute.
	ctx, cancel := context.WithTimeout(context.Background(), TorBootstrapTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tor", "-f", tmpFh.Name())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	log.Printf("Using bridge line %q.", bridgeLine)
	// Start tor but don't wait for the process to complete, so our call
	// returns right away.
	if err = cmd.Start(); err != nil {
		return err
	}

	// Read tor's log messages from stdout and try to figure out when/if tor
	// bootstrapped successfully.
	c := make(chan error)
	go func() {
		stdoutReader := bufio.NewReader(stdout)
		for {
			// If we hit our timeout, the tor process is terminated and we'll
			// end up with an error here.
			line, _, err := stdoutReader.ReadLine()
			if err != nil {
				log.Printf("Failed to read line from tor's stdout: %s", err)
				c <- err
				close(c)
				return
			}

			if torEncounteredError(string(line)) {
				if err := cmd.Process.Kill(); err != nil {
					log.Printf("Failed to kill process: %s", err)
				}
				// FIXME: Is %v correct here?
				c <- fmt.Errorf("%v", string(line))
				close(c)
				return
			}

			if torHasBootstrapped(string(line)) {
				log.Printf("Bootstrapping worked!")
				if err := cmd.Process.Kill(); err != nil {
					log.Printf("Failed to kill process: %s", err)
				}
				c <- nil
				close(c)
				return
			}
		}
	}()

	// FIXME: Use context to figure out if tor died on us.

	return <-c
}
