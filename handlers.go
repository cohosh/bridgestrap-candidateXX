package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"path"
	"time"

	"golang.org/x/time/rate"
)

var IndexPage string
var SuccessPage string
var FailurePage string

// BridgeTest represents the result of a bridge test, sent back to the client
// as JSON object.
type BridgeTest struct {
	Functional bool      `json:"functional"`
	LastTested time.Time `json:"last_tested"`
	Error      string    `json:"error,omitempty"`
}

// TestResult represents the result of a test.
type TestResult struct {
	Bridges map[string]*BridgeTest `json:"bridge_results"`
	Time    float64                `json:"time"`
	Error   string                 `json:"error,omitempty"`
}

// TestRequest represents a client's request to test a batch of bridges.
type TestRequest struct {
	BridgeLines []string `json:"bridge_lines"`
}

// limiter implements a rate limiter.  We allow 1 request per second on average
// with bursts of up to 5 requests per second.
var limiter = rate.NewLimiter(1, 5)

func NewTestResult() *TestResult {

	t := &TestResult{}
	t.Bridges = make(map[string]*BridgeTest)
	return t
}

// LoadHtmlTemplates loads all HTML templates from the given directory.
func LoadHtmlTemplates(dir string) {

	IndexPage = LoadHtmlTemplate(path.Join(dir, "index.html"))
	SuccessPage = LoadHtmlTemplate(path.Join(dir, "success.html"))
	FailurePage = LoadHtmlTemplate(path.Join(dir, "failure.html"))
}

// LoadHtmlTemplate reads the content of the given filename and returns it as
// string.  If the function is unable to read the file, it logs a fatal error.
func LoadHtmlTemplate(filename string) string {

	content, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}
	return string(content)
}

func SendResponse(w http.ResponseWriter, response string) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, response)
}

func SendHtmlResponse(w http.ResponseWriter, response string) {

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	SendResponse(w, response)
}

func SendJSONResponse(w http.ResponseWriter, response string) {

	w.Header().Set("Content-Type", "application/json")
	log.Printf("Test result: %s", response)
	SendResponse(w, response)
}

func Index(w http.ResponseWriter, r *http.Request) {

	SendHtmlResponse(w, IndexPage)
}

func testBridgeLines(bridgeLines []string) *TestResult {

	// Add cached bridge lines to the result.
	result := NewTestResult()
	remainingBridgeLines := []string{}
	numCached := 0
	for _, bridgeLine := range bridgeLines {
		if entry := cache.IsCached(bridgeLine); entry != nil {
			numCached++
			result.Bridges[bridgeLine] = &BridgeTest{
				Functional: entry.Error == "",
				LastTested: entry.Time,
				Error:      entry.Error,
			}
		} else {
			remainingBridgeLines = append(remainingBridgeLines, bridgeLine)
		}
	}

	// Test whatever bridges remain.
	if len(remainingBridgeLines) > 0 {
		log.Printf("%d bridge lines served from cache; testing remaining %d bridge lines.",
			numCached, len(remainingBridgeLines))

		start := time.Now()
		partialResult := torCtx.TestBridgeLines(remainingBridgeLines)
		result.Time = float64(time.Now().Sub(start).Seconds())
		result.Error = partialResult.Error

		// Cache partial test results and add them to our existing result object.
		for bridgeLine, bridgeTest := range partialResult.Bridges {
			cache.AddEntry(bridgeLine, errors.New(bridgeTest.Error), bridgeTest.LastTested)
			result.Bridges[bridgeLine] = bridgeTest
		}
	} else {
		log.Printf("All %d bridge lines served from cache.  No need for testing.", numCached)
	}

	// Log fraction of bridges that are functional.
	numFunctional, numDysfunctional := 0, 0
	for _, bridgeTest := range result.Bridges {
		if bridgeTest.Functional {
			numFunctional++
		} else {
			numDysfunctional++
		}
	}
	log.Printf("Tested %d bridges: %d (%.1f%%) functional; %d (%.1f%%) dysfunctional.",
		len(result.Bridges),
		numFunctional,
		float64(numFunctional)/float64(len(result.Bridges))*100,
		numDysfunctional,
		float64(numDysfunctional)/float64(len(result.Bridges))*100)

	return result
}

func BridgeState(w http.ResponseWriter, r *http.Request) {

	b, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		log.Printf("Failed to read HTTP body: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	req := &TestRequest{}
	if err := json.Unmarshal(b, &req); err != nil {
		log.Printf("Failed to unmarshal HTTP body %q: %s", b, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.BridgeLines) == 0 {
		log.Printf("Got request with no bridge lines.")
		http.Error(w, "no bridge lines given", http.StatusBadRequest)
		return
	}

	if len(req.BridgeLines) > MaxBridgesPerReq {
		log.Printf("Got %d bridges in request but we only allow <= %d.", len(req.BridgeLines), MaxBridgesPerReq)
		http.Error(w, fmt.Sprintf("maximum of %d bridge lines allowed", MaxBridgesPerReq), http.StatusBadRequest)
		return
	}

	log.Printf("Got %d bridge lines from %s.", len(req.BridgeLines), r.RemoteAddr)
	result := testBridgeLines(req.BridgeLines)

	jsonResult, err := json.Marshal(result)
	if err != nil {
		log.Printf("Bug: %s", err)
		http.Error(w, "failed to marshal test tesult", http.StatusInternalServerError)
		return
	}
	SendJSONResponse(w, string(jsonResult))
}

func BridgeStateWeb(w http.ResponseWriter, r *http.Request) {

	r.ParseForm()
	// Rate-limit Web requests to prevent someone from abusing this service
	// as a port scanner.
	if limiter.Allow() == false {
		SendHtmlResponse(w, "Rate limit exceeded.")
		return
	}
	bridgeLine := r.Form.Get("bridge_line")
	if bridgeLine == "" {
		SendHtmlResponse(w, "No bridge line given.")
		return
	}

	result := testBridgeLines([]string{bridgeLine})
	bridgeResult, exists := result.Bridges[bridgeLine]
	if !exists {
		log.Printf("Bug: Test result not part of our result map.")
		SendHtmlResponse(w, FailurePage)
		return
	}

	if bridgeResult.Functional {
		SendHtmlResponse(w, SuccessPage)
	} else {
		SendHtmlResponse(w, FailurePage)
	}
}
