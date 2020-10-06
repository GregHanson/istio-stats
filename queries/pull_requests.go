package queries

import (
	"github.com/shurcooL/githubv4"
)

type PullRequest struct {
	Title          githubv4.String
	Url            githubv4.String
	ReviewDecision githubv4.PullRequestReviewDecision
	LastEditedAt   githubv4.GitTimestamp
	Labels         struct {
		Nodes []struct {
			Name githubv4.String
		}
	} `graphql:"labels(first: 10)"`
}

type PullRequestNode struct {
	Node PullRequest
}

type PullRequestsQuery struct {
	Repository struct {
		PullRequests struct {
			Edges []PullRequestNode
		} `graphql:"pullRequests(first: 100, states: [OPEN], orderBy: {field: UPDATED_AT, direction: DESC})"`
	} `graphql:"repository(name: $name, owner: \"istio\")"`
	RateLimit struct {
		Limit     githubv4.Int
		Cost      githubv4.Int
		Remaining githubv4.Int
		ResetAt   githubv4.GitTimestamp
	}
}
