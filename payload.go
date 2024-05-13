package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

//Based on https://github.com/go-playground/webhooks/blob/master/github/github.go But api is out of data.

type Milestone struct {
	URL          string    `json:"url"`
	HTMLURL      string    `json:"html_url"`
	LabelsURL    string    `json:"labels_url"`
	ID           int64     `json:"id"`
	NodeID       string    `json:"node_id"`
	Number       int64     `json:"number"`
	State        string    `json:"state"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Creator      User      `json:"creator"`
	OpenIssues   int64     `json:"open_issues"`
	ClosedIssues int64     `json:"closed_issues"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ClosedAt     time.Time `json:"closed_at"`
	DueOn        time.Time `json:"due_on"`
}

type User struct {
	Login      string `json:"login"`
	ID         int64  `json:"id"`
	NodeID     string `json:"node_id"`
	AvatarURL  string `json:"avatar_url"`
	GravatarID string `json:"gravatar_id"`
	Type       string `json:"type"`
	SiteAdmin  bool   `json:"site_admin"`
}
type Reference struct {
	Label string     `json:"label"`
	Ref   string     `json:"ref"`
	Sha   string     `json:"sha"`
	User  User       `json:"user"`
	Repo  Repository `json:"repo"`
}

type Repository struct {
	ID                        int64     `json:"id"`
	NodeID                    string    `json:"node_id"`
	Name                      string    `json:"name"`
	FullName                  string    `json:"full_name"`
	Owner                     User      `json:"owner"`
	Private                   bool      `json:"private"`
	HTMLURL                   string    `json:"html_url"`
	Description               string    `json:"description"`
	Fork                      bool      `json:"fork"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
	PushedAt                  time.Time `json:"pushed_at"`
	Homepage                  *string   `json:"homepage"`
	Size                      int64     `json:"size"`
	StargazersCount           int64     `json:"stargazers_count"`
	WatchersCount             int64     `json:"watchers_count"`
	Language                  *string   `json:"language"`
	HasIssues                 bool      `json:"has_issues"`
	HasDownloads              bool      `json:"has_downloads"`
	HasProjects               bool      `json:"has_projects"`
	HasWiki                   bool      `json:"has_wiki"`
	HasPages                  bool      `json:"has_pages"`
	HasDiscussions            bool      `json:"has_discussions"`
	ForksCount                int64     `json:"forks_count"`
	Archived                  bool      `json:"archived"`
	Disabled                  bool      `json:"disabled"`
	OpenIssuesCount           int64     `json:"open_issues_count"`
	AllowForking              bool      `json:"allow_forking"`
	IsTemplate                bool      `json:"is_template"`
	WebCommitSignoffRequired  bool      `json:"web_commit_signoff_required"`
	Topics                    []string  `json:"topics"`
	Visibility                string    `json:"visibility"`
	Forks                     int64     `json:"forks"`
	OpenIssues                int64     `json:"open_issues"`
	Watchers                  int64     `json:"watchers"`
	DefaultBranch             string    `json:"default_branch"`
	AllowSquashMerge          bool      `json:"allow_squash_merge"`
	AllowMergeCommit          bool      `json:"allow_merge_commit"`
	AllowRebaseMerge          bool      `json:"allow_rebase_merge"`
	AllowAutoMerge            bool      `json:"allow_auto_merge"`
	DeleteBranch_onMerge      bool      `json:"delete_branch_on_merge"`
	AllowUpdateBranch         bool      `json:"allow_update_branch"`
	UseSquashPrTitleAsDefault bool      `json:"use_squash_pr_title_as_default"`
	SquashMergeCommitMessage  string    `json:"squash_merge_commit_message"`
	SquashMergeCommit_title   string    `json:"squash_merge_commit_title"`
	MergeCommitMessage        string    `json:"merge_commit_message"`
	MergeCommitTitle          string    `json:"merge_commit_title"`
}

type PullRequest struct {
	URL            string     `json:"url"`
	ID             int64      `json:"id"`
	NodeID         string     `json:"node_id"`
	Number         int64      `json:"number"`
	State          string     `json:"state"`
	Locked         bool       `json:"locked"`
	Title          string     `json:"title"`
	User           User       `json:"user"`
	Body           string     `json:"body"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ClosedAt       *time.Time `json:"closed_at"`
	MergedAt       *time.Time `json:"merged_at"`
	MergeCommitSha *string    `json:"merge_commit_sha"`
	Assignee       *User      `json:"assignee,omitempty"`
	Assignees      *[]User    `json:"assignees,omitempty"`
	Draft          bool       `json:"draft"`

	RequestedReviewers  []User    `json:"requested_reviewers,omitempty"`
	Labels              []Label   `json:"labels,omitempty"`
	RequestedTeam       *Team     `json:"requested_team,omitempty"`
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

func (pr *PullRequest) generateUUID() string {
	h := md5.New()
	generateString := fmt.Sprintf("%v%v%v%v", pr.Base.Repo.FullName, pr.ID, pr.Number, pr.State)
	h.Write([]byte(generateString))
	bs := h.Sum(nil)
	u, err := uuid.FromBytes(bs)
	if err != nil {
		debugLogger.Debug("generateUUID", "err", err)
		return uuid.New().String()
	}
	return u.String()
}

type PullRequestEvent struct {
	Timestamp   time.Time    `json:"timestamp"`
	Action      string       `json:"action"`
	Number      int64        `json:"number"`
	PullRequest *PullRequest `json:"pull_request"`
	Changes     *struct {
		Title *struct {
			From string `json:"from"`
		} `json:"title"`
		Body *struct {
			From string `json:"from"`
		} `json:"body"`
	} `json:"changes"`
	Repository        Repository `json:"repository"`
	Label             Label      `json:"label"`
	Sender            User       `json:"sender"`
	Assignee          *User      `json:"assignee,omitempty"`
	RequestedReviewer *User      `json:"requested_reviewer,omitempty"`
	RequestedTeam     *Team      `json:"requested_team,omitempty"`
	Installation      struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}
type Label struct {
	ID          int64  `json:"id"`
	NodeID      string `json:"node_id"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Default     bool   `json:"default"`
}
type Team struct {
	Name        string `json:"name"`
	ID          int64  `json:"id"`
	NodeID      string `json:"node_id"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Privacy     string `json:"privacy"`
	URL         string `json:"url"`
	Permission  string `json:"permission"`
}

func (pr *PullRequestEvent) parse() ([]byte, error) {
	pr.Timestamp = time.Now()
	return json.Marshal(pr)
}
