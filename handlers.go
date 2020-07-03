package main

import (
	"encoding/json"
	"fmt"
	"golang.org/x/time/rate"
	"io/ioutil"
	"log"
	"net/http"
	"path"
	"time"
)

var IndexPage string
var SuccessPage string
var FailurePage string

// TestResult represents the result of a test, sent back to the client as JSON
// object.
type TestResult struct {
	Functional bool    `json:"functional"`
	Error      string  `json:"error"`
	Time       float64 `json:"time"`
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

func createJsonResult(err error, start time.Time) string {

	end := time.Now()
	result := &TestResult{
		Functional: err == nil,
		Error:      "",
		Time:       float64(end.Sub(start).Milliseconds()) / 1000}
	if err != nil {
		result.Error = err.Error()
	}

	jsonResult, err := json.Marshal(result)
	if err != nil {
		log.Printf("Bug: %s", err)
	}

	return string(jsonResult)
}

func BridgeState(w http.ResponseWriter, r *http.Request) {

	start := time.Now()
	r.ParseForm()
	isWebRequest := r.Form.Get("web_request") != ""
	if isWebRequest {
		// Rate-limit Web requests to prevent someone from abusing this service
		// as a port scanner.
		if limiter.Allow() == false {
			SendHtmlResponse(w, "Rate limit exceeded.")
			return
		}
	}

	bridgeLine := r.Form.Get("bridge_line")
	if bridgeLine == "" {
		if isWebRequest {
			SendHtmlResponse(w, "No bridge line given.")
		} else {
			SendJSONResponse(w, createJsonResult(fmt.Errorf("No bridge line given"), start))
		}
		return
	}

	err := bootstrapTorOverBridge(bridgeLine)
	if isWebRequest {
		if err := bootstrapTorOverBridge(bridgeLine); err == nil {
			SendHtmlResponse(w, SuccessPage)
		} else {
			SendHtmlResponse(w, FailurePage)
		}
	} else {
		SendJSONResponse(w, createJsonResult(err, start))
	}
}
