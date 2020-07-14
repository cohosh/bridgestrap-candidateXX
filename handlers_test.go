package main

import (
	"fmt"
	"testing"
	"time"
)

func TestCreateJsonResult(t *testing.T) {

	expected := `{"functional":false,"error":"test","time":1}`
	now := time.Now()
	then := now.Add(time.Duration(-1) * time.Second)
	json := createJsonResult(fmt.Errorf("test"), then)
	if json != expected {
		t.Errorf("Got unexpected JSON: %s", json)
	}

	expected = `{"functional":true,"time":1}`
	now = time.Now()
	then = now.Add(time.Duration(-1) * time.Second)
	json = createJsonResult(nil, then)
	if json != expected {
		t.Errorf("Got unexpected JSON: %s", json)
	}
}
