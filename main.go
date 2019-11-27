package main

import (
	"flag"
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

type Routes []Route

var routes = Routes{
	Route{
		"APITestBridge",
		"POST",
		"/api/test",
		APITestBridge,
	},
}

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

func main() {

	var addr string
	var web bool
	var certFilename, keyFilename string
	var cacheFile string

	flag.StringVar(&addr, "addr", ":5000", "Address to listen on.")
	flag.BoolVar(&web, "web", false, "Enable the web interface (in addition to the JSON API).")
	flag.StringVar(&certFilename, "cert", "", "TLS certificate file.")
	flag.StringVar(&keyFilename, "key", "", "TLS private key file.")
	flag.StringVar(&cacheFile, "cache", "bridgestrap-cache.bin", "Cache file that contains test results.")
	flag.Parse()

	var logOutput io.Writer = os.Stderr
	// Send the log output through our scrubber first.
	log.SetOutput(&safelog.LogScrubber{Output: logOutput})
	log.SetFlags(log.LstdFlags | log.LUTC)

	if web {
		log.Println("Enabling web interface.")
		routes = append(routes,
			Route{
				"TestBridge",
				"POST",
				"/test",
				TestBridge,
			})
		routes = append(routes,
			Route{
				"Index",
				"GET",
				"/",
				Index,
			})
	}

	if err := cache.ReadFromDisk(cacheFile); err != nil {
		log.Printf("Could not read cache because: %s", err)
	}
	// When catching SIGINT or SIGTERM, write our cache to disk before exiting.
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT)
	signal.Notify(signalChan, syscall.SIGTERM)
	go func() {
		<-signalChan
		if err := cache.WriteToDisk(cacheFile); err != nil {
			log.Printf("Could not write cache because: %s", err)
		}
		os.Exit(1)
	}()

	router := NewRouter()
	log.Printf("Starting service on port %s.", addr)
	if certFilename != "" && keyFilename != "" {
		log.Fatal(http.ListenAndServeTLS(addr, certFilename, keyFilename, router))
	} else {
		log.Fatal(http.ListenAndServe(addr, router))
	}
}
