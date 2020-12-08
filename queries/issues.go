package queries

import (
	"github.com/shurcooL/githubv4"
)

type PageInfo struct {
	HasNextPage bool
	EndCursor   githubv4.String
}

type Assignee struct {
	Email githubv4.String
	Name  githubv4.String
	Login githubv4.String
}

type Issue struct {
	Title        githubv4.String
	Url          githubv4.String
	State        githubv4.String
	ProjectCards struct {
		Nodes []struct {
			Column struct {
				Name githubv4.String
			}
		}
	}
	Labels struct {
		Nodes []struct {
			Name githubv4.String
		}
	} `graphql:"labels(first: 10)"`
	LastEditedAt githubv4.GitTimestamp
	Assignees    struct {
		Edges []struct {
			Node Assignee
		}
	} `graphql:"assignees(first: 10)"`
}

type IssueNode struct {
	Node Issue
}

type MilestoneIssueQuery struct {
	Repository struct {
		Milestone struct {
			Title  githubv4.String
			Issues struct {
				PageInfo PageInfo
				Edges    []IssueNode
			} `graphql:"issues(first: 1000, filterBy: {states: OPEN}, after: $commentsCursor)"`
		} `graphql:"milestone(number: 22)"`
	} `graphql:"repository(name: $name, owner: \"istio\")"`
	RateLimit struct {
		Limit     githubv4.Int
		Cost      githubv4.Int
		Remaining githubv4.Int
		ResetAt   githubv4.GitTimestamp
	}
}

type HistoricalIssueQuery struct {
	Repository struct {
		Issues struct {
			Edges []struct {
				Node struct {
					Title     githubv4.String
					CreatedAt githubv4.GitTimestamp
				}
			}
		} `graphql:"issues(first: 100, filterBy:{since:$date}, orderBy:{field:UPDATED_AT, direction:DESC})"`
	} `graphql:"repository(name: $name, owner: \"istio\")"`
	RateLimit struct {
		Limit     githubv4.Int
		Cost      githubv4.Int
		Remaining githubv4.Int
		ResetAt   githubv4.GitTimestamp
	}
}
