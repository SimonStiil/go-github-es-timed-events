package main

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func Test_ParseJson(t *testing.T) {
	client := new(http.Client)
	req, _ := http.NewRequest("GET", "https://raw.githubusercontent.com/go-playground/webhooks/master/testdata/github/pull-request.json", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Error(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status not OK, %v", resp.StatusCode)
	}
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
	}
	playload := new(PullRequestEvent)
	err = json.Unmarshal(bodyText, playload)
	if err != nil {
		t.Error(err)
	}
}
func Test_Test_UUID_Generation(t *testing.T) {
	setupTestlogging()
	client := new(http.Client)
	req, _ := http.NewRequest("GET", "https://raw.githubusercontent.com/go-playground/webhooks/master/testdata/github/pull-request.json", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Error(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status not OK, %v", resp.StatusCode)
	}
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
	}
	playload := new(PullRequestEvent)
	err = json.Unmarshal(bodyText, playload)
	if err != nil {
		t.Error(err)
	}
	wantUUID := "c39b05e9-4c8e-aa2e-8e6d-f3b47804e88d"
	uuid := playload.PullRequest.generateUUID()
	if uuid != wantUUID {
		t.Errorf("%v does not match %v", uuid, wantUUID)
	}
	t.Logf("uuid: %v", uuid)
}
