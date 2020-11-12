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

const (
	// Cache test results for one week.
	CacheValidity = 7 * 24 * time.Hour
)

var cacheMutex sync.Mutex
var cache TestCache = make(TestCache)

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

// TestCache maps a bridge's addr:port tuple to a cache entry.
type TestCache map[string]*CacheEntry

// WriteToDisk writes our test result cache to disk, allowing it to persist
// across program restarts.
func (tc *TestCache) WriteToDisk(cacheFile string) error {

	fh, err := os.Create(cacheFile)
	if err != nil {
		return err
	}
	defer fh.Close()

	enc := gob.NewEncoder(fh)
	cacheMutex.Lock()
	err = enc.Encode(*tc)
	if err == nil {
		log.Printf("Wrote cache with %d elements to %q.",
			len(*tc), cacheFile)
	}
	cacheMutex.Unlock()

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
	cacheMutex.Lock()
	err = dec.Decode(tc)
	if err == nil {
		log.Printf("Read cache with %d elements from %q.",
			len(*tc), cacheFile)
	}
	cacheMutex.Unlock()

	return err
}

// IsCached returns a cache entry if the given bridge line has been tested
// recently (as determined by CacheValidity), and nil otherwise.
func (tc *TestCache) IsCached(bridgeLine string) *CacheEntry {

	// First, prune expired cache entries.
	now := time.Now().UTC()
	cacheMutex.Lock()
	for index, entry := range *tc {
		if entry.Time.Before(now.Add(-CacheValidity)) {
			delete(*tc, index)
		}
	}
	cacheMutex.Unlock()

	addrPort, err := bridgeLineToAddrPort(bridgeLine)
	if err != nil {
		return nil
	}

	cacheMutex.Lock()
	var r *CacheEntry = (*tc)[addrPort]
	cacheMutex.Unlock()

	return r
}

// AddEntry adds an entry for the given bridge and test result to our cache.
func (tc *TestCache) AddEntry(bridgeLine string, result error) {

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
	cacheMutex.Lock()
	(*tc)[addrPort] = &CacheEntry{errorStr, time.Now()}
	cacheMutex.Unlock()
}
