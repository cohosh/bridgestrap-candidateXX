package main

import (
	"encoding/json"
	"fmt"
	"golang.org/x/time/rate"
	"io/ioutil"
	"log"
	"net/http"
	"path"
	"sync"
	"time"
)

var IndexPage string
var SuccessPage string
var FailurePage string
var reqChan = make(chan *TestRequest, 1000)

// TestResult represents the result of a test, sent back to the client as JSON
// object.
type TestResult struct {
	Functional bool    `json:"functional"`
	Error      string  `json:"error,omitempty"`
	Time       float64 `json:"time"`
}

type TestRequest struct {
	BridgeLine string `json:"bridge_line"`
	respChan   chan *TestResult
}

// limiter implements a rate limiter.  We allow 1 request per second on average
// with bursts of up to 5 requests per second.
var limiter = rate.NewLimiter(1, 5)

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

func dispatchRequests(shutdown chan bool, wg *sync.WaitGroup, numSeconds int) {

	log.Printf("Starting request dispatcher.")
	delay := time.Tick(time.Second * time.Duration(numSeconds))
	wg.Add(1)
	defer wg.Done()
	defer log.Printf("Stopping request dispatcher.")

	for {
		select {
		case <-shutdown:
			return
		case req := <-reqChan:
			log.Printf("Fetching next request; %d requests remain buffered.", len(reqChan))
			go processRequest(req)

			// Either wait for the delay to expire or the service to shut down;
			// whatever comes first.
			select {
			case <-shutdown:
				return
			case <-delay:
			}
		}
	}
}

func processRequest(req *TestRequest) {

	start := time.Now()
	err := bootstrapTorOverBridge(req.BridgeLine)
	end := time.Now()
	result := &TestResult{
		Functional: err == nil,
		Error:      "",
		Time:       float64(end.Sub(start).Milliseconds()) / 1000}
	if err != nil {
		result.Error = err.Error()
	}
	req.respChan <- result
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

	if req.BridgeLine == "" {
		log.Printf("Got request with empty bridge line.")
		http.Error(w, "no bridge line given", http.StatusBadRequest)
		return
	}

	var res *TestResult
	// Do we have the given bridge line cached?  If so, we can respond right
	// away.
	if entry := cache.IsCached(req.BridgeLine); entry != nil {
		res = &TestResult{
			Functional: entry.Error == "",
			Error:      entry.Error,
			Time:       0.0}
	} else {
		respChan := make(chan *TestResult)
		req.respChan = respChan
		reqChan <- req
		res = <-respChan
	}

	jsonResult, err := json.Marshal(res)
	if err != nil {
		log.Printf("Bug: %s", err)
		http.Error(w, "failed to marshal test tesult", http.StatusInternalServerError)
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
	if err := bootstrapTorOverBridge(bridgeLine); err == nil {
		SendHtmlResponse(w, SuccessPage)
	} else {
		SendHtmlResponse(w, FailurePage)
	}
}
