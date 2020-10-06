package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"git.torproject.org/pluggable-transports/snowflake.git/common/safelog"
	"github.com/gorilla/mux"
)

type Route struct {
	Name        string
	Method      string
	Pattern     string
	HandlerFunc http.HandlerFunc
}

var torCtx *TorContext

type Routes []Route

var routes = Routes{
	Route{
		"BridgeState",
		"GET",
		"/bridge-state",
		BridgeState,
	},
	Route{
		"BridgeStateWeb",
		"GET",
		"/result",
		BridgeStateWeb,
	},
}

// tmpDataDir contains the path to Tor's data directory.
var tmpDataDir string

// Logger logs when we receive requests, and the execution time of handling
// these requests.  We don't log client IP addresses or the given obfs4
// parameters.
func Logger(inner http.Handler, name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		inner.ServeHTTP(w, r)

		log.Printf(
			"%s\t%s\t%s\t%s",
			r.Method,
			r.RequestURI,
			name,
			time.Since(start),
		)
	})
}

// NewRouter creates and returns a new request router.
func NewRouter() *mux.Router {

	router := mux.NewRouter().StrictSlash(true)
	for _, route := range routes {
		var handler http.Handler

		handler = route.HandlerFunc
		handler = Logger(handler, route.Name)

		router.
			Methods(route.Method).
			Path(route.Pattern).
			Name(route.Name).
			Handler(handler)
	}

	return router
}

func printPrettyCache() {
	var shortError string
	var numFunctional int

	for bridgeLine, cacheEntry := range cache {
		shortError = cacheEntry.Error
		maxChars := 50
		if len(cacheEntry.Error) > maxChars {
			shortError = cacheEntry.Error[:maxChars]
		}
		if cacheEntry.Error == "" {
			numFunctional++
		}
		fmt.Printf("%-22s %-50s %s\n", bridgeLine, shortError, cacheEntry.Time)
	}
	if len(cache) > 0 {
		log.Printf("Found %d (%.2f%%) out of %d functional.\n", numFunctional,
			float64(numFunctional)/float64(len(cache))*100.0, len(cache))
	}
}

func main() {

	var err error
	var addr string
	var web, printCache bool
	var certFilename, keyFilename string
	var cacheFile string
	var templatesDir string
	var numSecs int

	flag.StringVar(&addr, "addr", ":5000", "Address to listen on.")
	flag.BoolVar(&web, "web", false, "Enable the web interface (in addition to the JSON API).")
	flag.BoolVar(&printCache, "print-cache", false, "Print the given cache file and exit.")
	flag.StringVar(&certFilename, "cert", "", "TLS certificate file.")
	flag.StringVar(&keyFilename, "key", "", "TLS private key file.")
	flag.StringVar(&cacheFile, "cache", "bridgestrap-cache.bin", "Cache file that contains test results.")
	flag.StringVar(&templatesDir, "templates", "templates", "Path to directory that contains our web templates.")
	flag.IntVar(&numSecs, "seconds", 0, "Number of seconds after two subsequent requests are handled.")
	flag.Parse()

	var logOutput io.Writer = os.Stderr
	// Send the log output through our scrubber first.
	if !printCache {
		log.SetOutput(&safelog.LogScrubber{Output: logOutput})
	}
	log.SetFlags(log.LstdFlags | log.LUTC)

	LoadHtmlTemplates(templatesDir)

	if web {
		log.Println("Enabling web interface.")
		routes = append(routes,
			Route{
				"Index",
				"GET",
				"/",
				Index,
			})
	}

	if err = cache.ReadFromDisk(cacheFile); err != nil {
		log.Printf("Could not read cache: %s", err)
	}
	if printCache {
		printPrettyCache()
		return
	}

	torCtx = &TorContext{}
	if err = torCtx.Start(); err != nil {
		log.Printf("Failed to start Tor process: %s", err)
		return
	}

	var srv http.Server
	srv.Addr = addr
	srv.Handler = NewRouter()
	log.Printf("Starting service on port %s.", addr)
	go func() {
		if certFilename != "" && keyFilename != "" {
			srv.ListenAndServeTLS(certFilename, keyFilename)
		} else {
			srv.ListenAndServe()
		}
	}()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT)
	signal.Notify(signalChan, syscall.SIGTERM)

	log.Printf("Waiting for signal to shut down.")
	<-signalChan
	log.Printf("Received signal to shut down.")

	if err := torCtx.Stop(); err != nil {
		log.Printf("Failed to clean up after Tor: %s", err)
	}

	// Give our Web server a maximum of a minute to finish handling open
	// connections and shut down gracefully.
	t := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), t)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Failed to shut down Web server: %s", err)
	}

	if err := cache.WriteToDisk(cacheFile); err != nil {
		log.Printf("Failed to write cache to disk: %s", err)
	}
}
