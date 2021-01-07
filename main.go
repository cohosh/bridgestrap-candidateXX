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
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// BridgestrapVersion specifies bridgestrap's version.  The version number
	// is based on semantic versioning: https://semver.org
	BridgestrapVersion = "0.3.2"
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
		inner.ServeHTTP(w, r)
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
	router.Path("/metrics").Handler(promhttp.Handler())

	return router
}

func printPrettyCache() {
	var shortError string
	var numFunctional int

	for bridgeLine, cacheEntry := range cache.Entries {
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
	cacheLen := len(cache.Entries)
	if len(cache.Entries) > 0 {
		log.Printf("Found %d (%.2f%%) out of %d functional.\n", numFunctional,
			float64(numFunctional)/float64(cacheLen)*100.0, cacheLen)
	}
}

func main() {

	var err error
	var addr string
	var web, printCache, unsafeLogging, showVersion bool
	var certFilename, keyFilename string
	var cacheFile string
	var templatesDir string
	var torBinary string
	var testTimeout, cacheTimeout int
	var logFile string

	flag.StringVar(&addr, "addr", ":5000", "Address to listen on.")
	flag.BoolVar(&web, "web", false, "Enable the web interface (in addition to the JSON API).")
	flag.BoolVar(&printCache, "print-cache", false, "Print the given cache file and exit.")
	flag.BoolVar(&unsafeLogging, "unsafe", false, "Don't scrub IP addresses in log messages.")
	flag.BoolVar(&showVersion, "version", false, "Print bridgestrap's version and exit.")
	flag.StringVar(&certFilename, "cert", "", "TLS certificate file.")
	flag.StringVar(&keyFilename, "key", "", "TLS private key file.")
	flag.StringVar(&cacheFile, "cache", "bridgestrap-cache.bin", "Cache file that contains test results.")
	flag.StringVar(&templatesDir, "templates", "templates", "Path to directory that contains our web templates.")
	flag.StringVar(&torBinary, "tor", "tor", "Path to tor executable.")
	flag.StringVar(&logFile, "log", "", "File to write logs to.")
	flag.IntVar(&testTimeout, "test-timeout", 60, "Test timeout in seconds.")
	flag.IntVar(&cacheTimeout, "cache-timeout", 18, "Cache timeout in hours.")
	flag.Parse()

	if showVersion {
		fmt.Printf("bridgestrap version %s\n", BridgestrapVersion)
		return
	}

	var logOutput io.Writer = os.Stderr
	if logFile != "" {
		logFd, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatal(err)
		}
		logOutput = logFd
		log.SetOutput(logOutput)
		defer logFd.Close()
	}

	// Send the log output through our scrubber first.
	if !printCache && !unsafeLogging {
		log.SetOutput(&safelog.LogScrubber{Output: logOutput})
	}
	log.SetFlags(log.LstdFlags | log.LUTC)

	if web {
		log.Println("Enabling web interface.")
		LoadHtmlTemplates(templatesDir)
		routes = append(routes,
			Route{
				"Index",
				"GET",
				"/",
				Index,
			})
	}

	cache = NewTestCache()
	if err = cache.ReadFromDisk(cacheFile); err != nil {
		log.Printf("Could not read cache: %s", err)
	}
	cache.entryTimeout = time.Duration(cacheTimeout) * time.Hour
	log.Printf("Set cache timeout to %s.", cache.entryTimeout)
	if printCache {
		printPrettyCache()
		return
	}

	TorTestTimeout = time.Duration(testTimeout) * time.Second
	log.Printf("Setting Tor test timeout to %s.", TorTestTimeout)
	torCtx = &TorContext{TorBinary: torBinary}
	if err = torCtx.Start(); err != nil {
		log.Printf("Failed to start Tor process: %s", err)
		return
	}

	log.Printf("Initialising Prometheus metrics.")
	InitMetrics()

	var srv http.Server
	srv.Addr = addr
	srv.Handler = NewRouter()
	log.Printf("Starting service on port %s.", addr)
	go func() {
		if certFilename != "" && keyFilename != "" {
			err = srv.ListenAndServeTLS(certFilename, keyFilename)
		} else {
			err = srv.ListenAndServe()
		}
		if err != http.ErrServerClosed {
			log.Fatalf("Failed to run Web server: %s", err)
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
