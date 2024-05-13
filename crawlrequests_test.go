package main

import (
	"testing"
)

func Test_WebhookURL(t *testing.T) {
	setupTestlogging()
	WantedURL := "http://localhost/webhook"
	testConfig := ConfigGithub{Endpoint: "/webhook", PublicAddress: "http://localhost/"}
	testurl := testConfig.getWebHookURL()
	if testurl != WantedURL {
		t.Errorf("%v should be %v", testurl, WantedURL)
	}
	testConfig = ConfigGithub{Endpoint: "webhook", PublicAddress: "http://localhost/"}
	testurl = testConfig.getWebHookURL()
	if testurl != WantedURL {
		t.Errorf("%v should be %v", testurl, WantedURL)
	}
	testConfig = ConfigGithub{Endpoint: "/webhook", PublicAddress: "http://localhost"}
	testurl = testConfig.getWebHookURL()
	if testurl != WantedURL {
		t.Errorf("%v should be %v", testurl, WantedURL)
	}
	testConfig = ConfigGithub{Endpoint: "webhook", PublicAddress: "http://localhost"}
	testurl = testConfig.getWebHookURL()
	if testurl != WantedURL {
		t.Errorf("%v should be %v", testurl, WantedURL)
	}
}

func Test_ListRepostories(t *testing.T) {
	setupTestlogging()
	c := &Crawler{Config: ConfigGithub{Endpoint: "/webhook"}}
	c.Config.populateEnv()
	if c.Config.Token == "" {
		t.Log("Token not configured, Skipping Test")
		return
	}
	repos, err := c.ListRepositories()
	if err != nil {
		t.Error(err)
	}
	t.Logf("Found %v repositories", len(repos))
	for idx, repo := range repos {
		t.Logf("[%v] %v/%v %v", idx, repo.Owner.Login, repo.Name, repo.Private)
	}
}

func Test_ListPullRequests(t *testing.T) {
	setupTestlogging()

	c := &Crawler{Config: ConfigGithub{Endpoint: "/webhook"}}
	c.Config.populateEnv()
	if c.Config.Token == "" {
		t.Log("Token not configured, Skipping Test")
		return
	}
	pulls, next, err := c.getPullRequestsPage("https://api.github.com/repos/SimonStiil/turingpi-flux/pulls?state=all&per_page=50")
	if err != nil {
		t.Error(err)
	}
	if next != "" {
		t.Log("has more pages")
	}
	t.Logf("Found %v Pullrequests", len(pulls))
	for idx, pull := range pulls {
		t.Logf("[%v] (%v) %v %v", idx, pull.Number, pull.State, pull.Title)
	}
}

func Test_ListWebhooks(t *testing.T) {
	setupTestlogging()
	c := &Crawler{Config: ConfigGithub{Endpoint: "/webhook"}}
	c.Config.populateEnv()
	if c.Config.Token == "" {
		t.Log("Token not configured, Skipping Test")
		return
	}
	webhooks, next, err := c.getWebHooksPage("https://api.github.com/repos/SimonStiil/kube-auth-proxy/hooks")
	if err != nil {
		t.Error(err)
	}
	if next != "" {
		t.Log("has more pages")
	}
	t.Logf("Found %v repositories", len(webhooks))
	for idx, webhook := range webhooks {
		t.Logf("[%v] %v", idx, webhook.String())
	}
}
