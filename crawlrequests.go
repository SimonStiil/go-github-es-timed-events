package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/tomnomnom/linkheader"
)

var (
	ErrStatusNotAccepted  = errors.New("error, wrong status")
	ErrStatusUnauthorized = errors.New("error, not authorized")
)

type Crawler struct {
	Config    ConfigGithub
	ES        *Search
	next      int
	list      []Repository
	remaining int
	lowNotise bool
}

func (c *Crawler) Tick() {
	debugLogger.Debug("Tick Event")
	if c.list == nil || len(c.list) == 0 {
		list, err := c.ListRepositories()
		if err == ErrStatusUnauthorized {
			panic(err)
		}
		debugLogger.Debug("ListRepositories", "size", len(list))
		c.list = list
		c.next = 0
		newRepos := 0
		for idx, repo := range list {
			age := time.Since(repo.CreatedAt)
			if age.Hours() < 48 {
				newRepos += 1
			}
			debugLogger.Debug("list", "id", idx, "name", repo.FullName, "created", repo.CreatedAt, "age", age)
		}
		logger.Info("ListRepositories", "size", len(list), "new", newRepos)
	}
	if c.remaining < 2000 && !c.lowNotise {
		logger.Info("Low Quota", "remaining", c.remaining)
		c.lowNotise = true
	} else {
		c.lowNotise = false
		repository := c.list[c.next]
		prPageSize := ""
		if c.Config.PRPageSize > 0 {
			prPageSize = fmt.Sprintf("&per_page=%v", c.Config.PRPageSize)
		}
		URL := fmt.Sprintf("https://api.github.com/repos/%v/pulls?state=all%v", repository.FullName, prPageSize)
		debugLogger.Debug("do getPullRequestsPage", "name", repository.FullName, "URL", URL)
		pulls, _, err := c.getPullRequestsPage(URL)
		if err != nil {
			logger.Error("error getPullRequestsPage", "url", URL, "error", err)
			return
		}
		for idx, pull := range pulls {
			push := true
			age := time.Since(pull.CreatedAt)
			if pull.State == "closed" {
				age = time.Since(*pull.ClosedAt)
				if age.Hours() > 48 {
					debugLogger.Debug("Not Pushing Old closed PR", "id", idx, "number", pull.Number, "title", pull.Title, "age", age)
					push = false
				}
			}
			if push {
				debugLogger.Debug("Pushing PR", "id", idx, "number", pull.Number, "title", pull.Title)
				event, err := pull.toPullRequestEvent()
				if err != nil {
					logger.Error("error converting PR to PullRequestEvent", "repo", repository.FullName, "id", idx, "number", pull.Number, "title", pull.Title)
					continue
				}
				byteArray, err := event.parse()
				if err != nil {
					logger.Error("error parsing payload to json", "repo", repository.FullName, "id", idx, "number", pull.Number, "title", pull.Title, "error", err)
				}
				ctx := context.Background()
				uuid := pull.generateUUID()
				res, err := esapi.CreateRequest{
					Index:      config.Elastic.Index,
					DocumentID: uuid,
					Body:       bytes.NewReader(byteArray),
				}.Do(ctx, c.ES.esClient)
				if err != nil {
					logger.Error("error doing es request", "error", err)
					continue
				}
				if res.IsError() {
					if res.StatusCode == 409 {
						debugLogger.Debug("Pushed PR - Already exists", "id", idx, "number", pull.Number, "uuid", uuid)
						continue
					}
					printESError("error posting value", res)
					continue
				}
				debugLogger.Debug("Pushed PR", "id", idx, "number", pull.Number, "uuid", uuid)
				logger.Info("Pushed PR", "repo", repository.FullName, "number", pull.Number, "title", pull.Title, "state", pull.State, "age", age, "documentID", uuid)
			}

		}

		//testHook := repository.CreatedAt.After(time.Now().Add(-48 * time.Hour))
		err = c.updateWebHooks(repository.FullName)
		if err != nil {
			logger.Error("error updateWebHooks", "repository", repository.FullName, "error", err)
		}
		if c.next+1 == len(c.list) {
			c.next = 0
			c.list = nil
		} else {
			c.next += 1
		}
	}
}

func (c *Crawler) ListRepositories() ([]Repository, error) {
	//https://docs.github.com/en/rest/repos/repos?apiVersion=2022-11-28#list-repositories-for-the-authenticated-user
	// Lists repositories that the authenticated user has explicit permission (:read, :write, or :admin) to access.
	// The token must have the following permission set: metadata:read
	debugLogger.Debug("ListRepositories start")

	var r []Repository
	next := "https://api.github.com/user/repos"
	for next != "" {
		page, nextURL, err := c.getRepositoriesPage(next)
		if err != nil {
			logger.Error("error getting page", "page", next, "error", err)
			return nil, err
		}

		debugLogger.Debug("got page", "page", next, "size", len(page))
		next = nextURL
		r = append(r, page...)
	}
	return r, nil
}
func (c *Crawler) getRepositoriesPage(url string) ([]Repository, string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	req.Header = http.Header{
		"Accept":               {"application/vnd.github+json"},
		"X-GitHub-Api-Version": {"2022-11-28"},
		"Authorization":        {"Bearer " + c.Config.Token},
	}
	resp, err := client.Do(req)
	if err != nil {
		debugLogger.Debug("error doingRequest", "req", req)
		return nil, "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "", ErrStatusUnauthorized
	}
	bodyText, err := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		debugLogger.Debug("StatusError", "statusCode", resp.StatusCode, "body", bodyText)
		return nil, "", ErrStatusNotAccepted
	}
	linkUnparsed := resp.Header.Get("Link")
	links := linkheader.Parse(linkUnparsed)
	nextLinks := links.FilterByRel("next")
	debugLogger.Debug("ratelimit", "content", c.getRateLimits(resp.Header))
	nextURL := ""
	if len(nextLinks) > 0 {
		nextURL = nextLinks[0].URL
	}
	if err != nil {
		logger.Error("error reading body", "error", err)
		return nil, "", err
	}
	var r []Repository
	if err := json.Unmarshal(bodyText, &r); err != nil {
		logger.Error("unable to unmarshal body", "body", bodyText)
		return r, "", err
	}
	return r, nextURL, err
}

func (c *Crawler) getRateLimits(header http.Header) *RateLimit {
	used := convertVar(header, "X-Ratelimit-Used")
	remaining := convertVar(header, "X-Ratelimit-Remaining")
	c.remaining = *remaining
	limit := convertVar(header, "X-Ratelimit-Limit")
	var reset *time.Time
	resetValue := header.Get("X-Ratelimit-Reset")
	resetInt, err := strconv.ParseInt(resetValue, 10, 64)
	if err != nil {
		logger.Error("error ParseInt", "variable", "X-Ratelimit-Reset", "value", resetValue, "error", err)
	} else {
		resetTime := time.Unix(resetInt, 0)
		reset = &resetTime
	}

	ratelimit := &RateLimit{
		Used:      used,
		Remaining: remaining,
		Total:     limit,
		Reset:     reset,
	}
	ratelimit.setGauges()
	return ratelimit
}

func convertVar(header http.Header, variable string) *int {
	value := header.Get(variable)
	res, err := strconv.Atoi(value)
	if err != nil {
		logger.Error("error converting", "variable", variable, "value", value, "error", err)
		return nil

	}
	return &res
}

type RateLimit struct {
	Used      *int       `json:"used,omitempty"`
	Remaining *int       `json:"remaining,omitempty"`
	Total     *int       `json:"total,omitempty"`
	Reset     *time.Time `json:"reset,omitempty"`
}

func (r *RateLimit) String() string {
	return fmt.Sprintf("used: %v, remaining: %v, total: %v, resets: %v", *r.Used, *r.Remaining, *r.Total, r.Reset)
}
func (r *RateLimit) setGauges() {
	ratelimit_used.Set(float64(*r.Used))
	ratelimit_remaining.Set(float64(*r.Remaining))
	ratelimit_total.Set(float64(*r.Total))
	ratelimit_reset.Set(float64(r.Reset.Unix() - time.Now().Unix()))
}

type WebHook struct {
	Type          *string       `json:"type,omitempty"`
	ID            *int64        `json:"id,omitempty"`
	Name          string        `json:"name,omitempty"`
	Active        bool          `json:"active,omitempty"`
	Events        *[]string     `json:"events,omitempty"`
	Config        WebHookConfig `json:"config,omitempty"`
	UpdatedAt     *time.Time    `json:"updated_at,omitempty"`
	CreatedAt     *time.Time    `json:"created_at,omitempty"`
	URL           *string       `json:"url,omitempty"`
	TestURL       *string       `json:"test_url,omitempty"`
	PingURL       *string       `json:"ping_url,omitempty"`
	DeliveriesURL *string       `json:"deliveries_url,omitempty"`
	LastResponse  *LastResponse `json:"last_response,omitempty"`
}

func (webhook *WebHook) String() string {
	return fmt.Sprintf("ID: %v, URL: %v, LastResponseCode: %v", *webhook.ID, webhook.Config.URL, webhook.LastResponse.Code)
}

type LastResponse struct {
	Code    int    `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type WebHookConfig struct {
	ContentType string  `json:"content_type"`
	InsecureSSL *string `json:"insecure_ssl,omitempty"`
	URL         string  `json:"url"`
}

/*
	type PullRequest struct {
		URL    string `json:"url"`
		ID     int64  `json:"id"`
		NodeID string `json:"node_id"`
		Number int64  `json:"number"`
		State  string `json:"state"`
		Locked bool   `json:"locked"`
		Title  string `json:"title"`
		User   User   `json:"user"`
		Body   string `json:"body"`
		CreatedAt          time.Time  `json:"created_at"`
		UpdatedAt          time.Time  `json:"updated_at"`
		ClosedAt           *time.Time `json:"closed_at"`
		MergedAt           *time.Time `json:"merged_at"`
		MergeCommitSha     *string    `json:"merge_commit_sha"`
		Assignee           *User      `json:"assignee,omitempty"`
		Assignees          *[]User    `json:"assignees,omitempty"`
		Draft              bool       `json:"draft"`
		RequestedReviewers []User     `json:"requested_reviewers,omitempty"`
		Labels []struct {
			ID          int64  `json:"id"`
			NodeID      string `json:"node_id"`
			Description string `json:"description"`
			URL         string `json:"url"`
			Name        string `json:"name"`
			Color       string `json:"color"`
			Default     bool   `json:"default"`
		} `json:"labels"`
		RequestedTeam      struct {
			Name        string `json:"name"`
			ID          int64  `json:"id"`
			NodeID      string `json:"node_id"`
			Slug        string `json:"slug"`
			Description string `json:"description"`
			Privacy     string `json:"privacy"`
			URL         string `json:"url"`
			Permission  string `json:"permission"`
		} `json:"requested_team"`
		Head                Reference `json:"head"`
		Base                Reference `json:"base"`
		AuthorAssociation   string    `json:"author_association"`
		Merged              bool      `json:"merged"`
		Mergeable           *bool     `json:"mergeable"`
		Rebaseable          bool      `json:"rebaseable"`
		MergeableState      string    `json:"mergeable_state"`
		MergedBy            *User     `json:"merged_by"`
		Comments            int64     `json:"comments"`
		ReviewComments      int64     `json:"review_comments"`
		MaintainerCanModify bool      `json:"maintainer_can_modify"`
		Commits             int64     `json:"commits"`
		Additions           int64     `json:"additions"`
		Deletions           int64     `json:"deletions"`
		ChangedFiles        int64     `json:"changed_files"`
	}
*/
func (pr *PullRequest) toPullRequestEvent() (*PullRequestEvent, error) {
	pre := &PullRequestEvent{
		Timestamp:   pr.CreatedAt,
		Action:      "periodic_pull",
		Number:      pr.Number,
		PullRequest: pr,
		Repository:  pr.Base.Repo,
		Sender:      pr.Head.User,
		Assignee:    pr.Assignee,
	}
	return pre, nil
}

func (c *Crawler) getPullRequestsPage(url string) ([]PullRequest, string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	req.Header = http.Header{
		"Accept":               {"application/vnd.github+json"},
		"X-GitHub-Api-Version": {"2022-11-28"},
		"Authorization":        {"Bearer " + c.Config.Token},
	}
	resp, err := client.Do(req)
	if err != nil {
		debugLogger.Debug("error doingRequest", "req", req)
		return nil, "", err
	}

	linkUnparsed := resp.Header.Get("Link")
	links := linkheader.Parse(linkUnparsed)
	nextLinks := links.FilterByRel("next")
	debugLogger.Debug("ratelimit", "content", c.getRateLimits(resp.Header))
	nextURL := ""
	if len(nextLinks) > 0 {
		nextURL = nextLinks[0].URL
	}
	bodyText, err := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "", ErrStatusUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		debugLogger.Debug("StatusError", "statusCode", resp.StatusCode, "body", bodyText)
		return nil, "", ErrStatusNotAccepted
	}
	if err != nil {
		logger.Error("error reading body", "error", err)
		return nil, "", err
	}
	var r []PullRequest
	if err := json.Unmarshal(bodyText, &r); err != nil {
		logger.Error("unable to unmarshal body", "body", bodyText)
		return r, "", err
	}
	return r, nextURL, err
}

func (c *Crawler) updateWebHooks(repoFullName string) error {
	webhookURL := c.Config.getWebHookURL()
	if webhookURL != "" {
		hooksURL := fmt.Sprintf("https://api.github.com/repos/%v/hooks", repoFullName)
		webhooks, _, err := c.getWebHooksPage(hooksURL)
		if err != nil {
			return err
		}
		newWebhookObject := WebHook{Name: "web", Active: true, Events: &[]string{"pull_request"}, Config: WebHookConfig{URL: c.Config.getWebHookURL(), ContentType: "json"}}
		found := false
		for _, webhook := range webhooks {
			if webhook.Config.URL == newWebhookObject.Config.URL {
				found = true
				debugLogger.Debug("webhook already exists skipping", "repo-full-name", repoFullName)
			}
		}
		if !found {
			newWebhook, err := c.createWebHook(hooksURL, newWebhookObject)
			if err != nil {
				return err
			}
			debugLogger.Debug("webhook created: " + newWebhook.String())
		}
		return err
	}
	return nil
}

func (c *Crawler) getWebHooksPage(url string) ([]WebHook, string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	req.Header = http.Header{
		"Accept":               {"application/vnd.github+json"},
		"X-GitHub-Api-Version": {"2022-11-28"},
		"Authorization":        {"Bearer " + c.Config.Token},
	}
	resp, err := client.Do(req)
	if err != nil {
		debugLogger.Debug("error doingRequest", "req", req)
		return nil, "", err
	}

	linkUnparsed := resp.Header.Get("Link")
	links := linkheader.Parse(linkUnparsed)
	nextLinks := links.FilterByRel("next")
	debugLogger.Debug("ratelimit", "content", c.getRateLimits(resp.Header))
	nextURL := ""
	if len(nextLinks) > 0 {
		nextURL = nextLinks[0].URL
	}
	bodyText, err := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "", ErrStatusUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		debugLogger.Debug("StatusError", "statusCode", resp.StatusCode, "body", bodyText)
		return nil, "", ErrStatusNotAccepted
	}
	if err != nil {
		logger.Error("error reading body", "error", err)
		return nil, "", err
	}
	var r []WebHook
	if err := json.Unmarshal(bodyText, &r); err != nil {
		logger.Error("unable to unmarshal body", "body", bodyText)
		return r, "", err
	}
	return r, nextURL, err
}

func (c *Crawler) createWebHook(url string, webhook WebHook) (*WebHook, error) {
	client := &http.Client{}
	marshalled, err := json.Marshal(webhook)
	if err != nil {
		logger.Error("Impossible to marshall Webhook: %s", err)
		return nil, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(marshalled))
	if err != nil {
		panic(err)
	}
	req.Header = http.Header{
		"Accept":               {"application/vnd.github+json"},
		"X-GitHub-Api-Version": {"2022-11-28"},
		"Authorization":        {"Bearer " + c.Config.Token},
	}
	resp, err := client.Do(req)
	if err != nil {
		debugLogger.Debug("error doingRequest", "req", req)
		return nil, err
	}
	debugLogger.Debug("ratelimit", "content", c.getRateLimits(resp.Header))
	bodyText, err := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrStatusUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		debugLogger.Debug("StatusError", "statusCode", resp.StatusCode, "body", bodyText)
		return nil, ErrStatusNotAccepted
	}
	if err != nil {
		logger.Error("error reading body", "error", err)
		return nil, err
	}
	var r *WebHook
	if err := json.Unmarshal(bodyText, r); err != nil {
		logger.Error("unable to unmarshal body", "body", bodyText)
		return r, err
	}
	return r, err
}
