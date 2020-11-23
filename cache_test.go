package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"testing"
	"time"
)

func init() {
	InitMetrics()
}

func NewCache() *TestCache {
	return &TestCache{
		Entries:      make(map[string]*CacheEntry),
		EntryTimeout: 24 * time.Hour,
	}
}

func TestCacheFunctions(t *testing.T) {

	cache := NewCache()
	bridgeLine := "obfs4 127.0.0.1:1 cert=foo iat-mode=0"

	e := cache.IsCached(bridgeLine)
	if e != nil {
		t.Errorf("Cache is empty but marks bridge line as existing.")
	}

	cache.AddEntry(bridgeLine, nil, time.Now().UTC())
	e = cache.IsCached(bridgeLine)
	if e == nil {
		t.Errorf("Could not retrieve existing element from cache.")
	}

	testError := fmt.Errorf("bridge is on fire")
	cache.AddEntry(bridgeLine, testError, time.Now().UTC())
	e = cache.IsCached(bridgeLine)
	if e.Error != testError.Error() {
		t.Errorf("Got test result %q but expected %q.", e.Error, testError)
	}

	// A bogus bridge line shouldn't make it into the cache.
	cache = NewCache()
	bogusBridgeLine := "bogus-bridge-line"
	cache.AddEntry(bogusBridgeLine, errors.New("bogus-error"), time.Now().UTC())
	if len(cache.Entries) != 0 {
		t.Errorf("Bogus bridge line made it into cache.")
	}

	e = cache.IsCached(bogusBridgeLine)
	if e != nil {
		t.Errorf("Got non-nil cache entry for bogus bridge line.")
	}
}

func TestCacheFracFunctional(t *testing.T) {

	cache := NewCache()

	cache.AddEntry("1.1.1.1:1", nil, time.Now().UTC())
	cache.AddEntry("2.2.2.2:2", nil, time.Now().UTC())
	cache.AddEntry("3.3.3.3:3", nil, time.Now().UTC())
	cache.AddEntry("4.4.4.4:4", errors.New("error"), time.Now().UTC())

	expected := 0.75
	if cache.FracFunctional() != expected {
		t.Errorf("Expected fraction %.2f but got %.2f.", expected, cache.FracFunctional())
	}
}

func TestCacheExpiration(t *testing.T) {

	cache := NewCache()

	const shortForm = "2006-Jan-02"
	expiry, _ := time.Parse(shortForm, "2000-Jan-01")
	bridgeLine1 := "1.1.1.1:1111"
	cache.Entries[bridgeLine1] = &CacheEntry{"", expiry}

	bridgeLine2 := "2.2.2.2:2222"
	cache.Entries[bridgeLine2] = &CacheEntry{"", time.Now().UTC()}

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
	cache := NewCache()
	for i := 0; i < numCacheEntries; i++ {
		cache.AddEntry(getRandAddrPort(), getRandError(), time.Now().UTC())
	}

	// How long does it take to iterate over numCacheEntries cache entries?
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.IsCached("invalid bridge line")
	}
}

func TestCacheSerialisation(t *testing.T) {

	cache := NewCache()
	testError := fmt.Errorf("foo")
	cache.AddEntry("1.1.1.1:1", testError, time.Now().UTC())
	cache.AddEntry("2.2.2.2:2", fmt.Errorf("bar"), time.Now().UTC())

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

	if len(cache.Entries) != 2 {
		t.Errorf("Cache supposed to contain but two elements but has %d.", len(cache.Entries))
	}

	e1 := cache.IsCached("1.1.1.1:1")
	if e1 == nil {
		t.Errorf("Cache element supposed to exist but doesn't.")
	}
	if e1.Error != testError.Error() {
		t.Errorf("Error string expected to be %q but is %q.", testError, e1.Error)
	}

	// Test errors when reading/writing bogus files.
	if err = cache.ReadFromDisk("/f/o/o/b/a/r"); err == nil {
		t.Errorf("Failed to return error when reading bogus file.")
	}
	if err = cache.WriteToDisk("/f/o/o/b/a/r"); err == nil {
		t.Errorf("Failed to return error when writing bogus file.")
	}
}

func TestCacheConcurrency(t *testing.T) {

	cache := NewCache()
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
			cache.AddEntry(fmt.Sprintf("%s:1234", ipAddr.String()), nil, time.Now().UTC())
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
