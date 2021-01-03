package main

import (
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"regexp"
	"sync"
	"time"
)

var cache *TestCache

// Regular expression that captures the address:port part of a bridge line (for
// both IPv4 and IPv6 addresses).
var AddrPortBridgeLine = regexp.MustCompile(`[0-9a-z\[\]\.:]+:[0-9]{1,5}`)

// CacheEntry represents an entry in our cache of bridges that we recently
// tested.  Error is nil if a bridge works, and otherwise holds an error
// string.  Time determines when we tested the bridge.
type CacheEntry struct {
	// We're using a string instead of an error here because golang's gob
	// package doesn't know how to deal with an error:
	// <https://github.com/golang/go/issues/23340>
	Error string
	Time  time.Time
}

type TestCache struct {
	// Entries maps a bridge's addr:port tuple to a cache entry.
	Entries map[string]*CacheEntry
	// entryTimeout determines how long a cache entry is valid for.
	entryTimeout time.Duration
	l            sync.Mutex
}

// NewTestCache returns a new test cache.
func NewTestCache() *TestCache {
	return &TestCache{Entries: make(map[string]*CacheEntry)}
}

// bridgeLineToAddrPort takes a bridge line as input and returns a string
// consisting of the bridge's addr:port (for both IPv4 and IPv6 addresses).
func bridgeLineToAddrPort(bridgeLine string) (string, error) {

	result := string(AddrPortBridgeLine.Find([]byte(bridgeLine)))
	if result == "" {
		return result, fmt.Errorf("could not extract addr:port from bridge line")
	} else {
		return result, nil
	}
}

// FracFunctional returns the fraction of bridges currently in the cache that
// are functional.
func (tc *TestCache) FracFunctional() float64 {

	tc.l.Lock()
	defer tc.l.Unlock()

	if len((*tc).Entries) == 0 {
		return 0
	}

	numFunctional := 0
	for _, entry := range (*tc).Entries {
		if entry.Error == "" {
			numFunctional++
		}
	}

	return float64(numFunctional) / float64(len((*tc).Entries))
}

// WriteToDisk writes our test result cache to disk, allowing it to persist
// across program restarts.
func (tc *TestCache) WriteToDisk(cacheFile string) error {

	fh, err := os.Create(cacheFile)
	if err != nil {
		return err
	}
	defer fh.Close()

	enc := gob.NewEncoder(fh)
	tc.l.Lock()
	err = enc.Encode(*tc)
	if err == nil {
		log.Printf("Wrote cache with %d elements to %q.",
			len((*tc).Entries), cacheFile)
	}
	tc.l.Unlock()

	return err
}

// ReadFromDisk reads our test result cache from disk.
func (tc *TestCache) ReadFromDisk(cacheFile string) error {

	fh, err := os.Open(cacheFile)
	if err != nil {
		return err
	}
	defer fh.Close()

	dec := gob.NewDecoder(fh)
	tc.l.Lock()
	err = dec.Decode(tc)
	if err == nil {
		log.Printf("Read cache with %d elements from %q.",
			len((*tc).Entries), cacheFile)
	}
	tc.l.Unlock()

	return err
}

// IsCached returns a cache entry if the given bridge line has been tested
// recently (as determined by entryTimeout), and nil otherwise.
func (tc *TestCache) IsCached(bridgeLine string) *CacheEntry {

	// First, prune expired cache entries.
	now := time.Now().UTC()
	tc.l.Lock()
	for index, entry := range (*tc).Entries {
		if entry.Time.Before(now.Add(-(*tc).entryTimeout)) {
			delete((*tc).Entries, index)
		}
	}
	tc.l.Unlock()

	addrPort, err := bridgeLineToAddrPort(bridgeLine)
	if err != nil {
		return nil
	}

	tc.l.Lock()
	var r *CacheEntry = (*tc).Entries[addrPort]
	tc.l.Unlock()

	return r
}

// AddEntry adds an entry for the given bridge, test result, and test time to
// our cache.
func (tc *TestCache) AddEntry(bridgeLine string, result error, lastTested time.Time) {

	addrPort, err := bridgeLineToAddrPort(bridgeLine)
	if err != nil {
		return
	}

	var errorStr string
	if result == nil {
		errorStr = ""
	} else {
		errorStr = result.Error()
	}
	tc.l.Lock()
	(*tc).Entries[addrPort] = &CacheEntry{errorStr, lastTested}
	tc.l.Unlock()

	metrics.FracFunctional.Set((*tc).FracFunctional())
}
