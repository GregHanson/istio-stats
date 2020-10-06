package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/GregHanson/istio-stats/queries"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

var (
	allIssues       []queries.IssueNode
	allPullRequests []queries.PullRequestNode
	staleFactor     time.Duration
	server          *sheets.Service
	token           string
	googleCreds     string
	days            int64
	sheetID         string
	priorities      map[githubv4.String][]*queries.Issue
	client          *githubv4.Client
)

func init() {
	flag.Int64Var(&days, "stale-factor", 3, "number of days that are used to mark a priority item stale")
	flag.StringVar(&token, "token", os.Getenv("GITHUB_TOKEN"), "token to be used for authenticating with Github API")
	flag.StringVar(&googleCreds, "creds", "./credentials.json", "credential file to be used for authenticating with Google Sheets API")
	flag.StringVar(&sheetID, "sheet", "1OzEFguruX9vB6IJot2hk5f5B7D4N48VAOO61bEJQXy4", "Google Sheets spreadsheet ID to push dashboard to")
}

func main() {

	staleFactor = time.Hour * 24 * time.Duration(days)
	priorities = map[githubv4.String][]*queries.Issue{}
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	httpClient := oauth2.NewClient(context.Background(), src)
	client = githubv4.NewClient(httpClient)

	getIssues()
	stats()
}

func getIssues() {
	q := queries.IssueQuery{}
	variables := map[string]interface{}{
		"commentsCursor": (*githubv4.String)(nil), // Null after argument to get first page.
		"name":           githubv4.String("istio"),
	}
	for {
		err := client.Query(context.Background(), &q, variables)
		if err != nil {
			fmt.Printf("%v\n", q)
			log.Fatalf("Query failed, err: %v", err)
		}
		allIssues = append(allIssues, q.Repository.Milestone.Issues.Edges...)
		if !q.Repository.Milestone.Issues.PageInfo.HasNextPage {
			break
		}
		variables["commentsCursor"] = githubv4.NewString(q.Repository.Milestone.Issues.PageInfo.EndCursor)
	}

	fmt.Printf("milestone: %v\n", q.Repository.Milestone.Title)
	fmt.Printf("Rate Limits:\n\tLimit:%v\n\tCost:%v\n\tRemaining:%v\n", q.RateLimit.Limit, q.RateLimit.Cost, q.RateLimit.Remaining)
	processIssues()
}

func processIssues() {
	for issueIndex, i := range allIssues {
		issue := i.Node
		for _, card := range issue.ProjectCards.Nodes {
			name := card.Column.Name
			if _, exists := priorities[name]; !exists {
				priorities[name] = []*queries.Issue{&allIssues[issueIndex].Node}
			} else {
				priorities[name] = append(priorities[name], &allIssues[issueIndex].Node)
			}
		}
	}

	var vr sheets.ValueRange

	for priority, issues := range priorities {
		fmt.Printf("Priority {%v} has {%v} items OPEN in it\n", priority, len(issues))
		for _, issue := range issues {
			stale := ""
			if !issue.LastEditedAt.Time.Add(staleFactor).Before(time.Now()) {
				fmt.Printf("\tStale: %v", issue.Title)
				stale = "STALE"
			}
			area := ""
			for _, l := range issue.Labels.Nodes {
				if strings.Contains(string(l.Name), "area/") {
					area = string(l.Name)
				}
			}
			myval := []interface{}{priority, issue.Url, issue.Title, area, getAssignees(issue), stale}
			vr.Values = append(vr.Values, myval)
		}
	}

	clearSheet("Issues!A2:F")
	updateSheet("Issues!A2:F", &vr)
}

func getAssignees(i *queries.Issue) string {
	retval := ""
	for _, owner := range i.Assignees.Edges {
		if retval == "" {
			retval = string(owner.Node.Name)
		} else {
			retval = retval + "," + string(owner.Node.Name)
		}
	}
	return retval
}

// TODO: escalation for issues and pulls

func stats() {
	var vr sheets.ValueRange
	processPullRequests(&vr)
	processDailyIssues(&vr)

	clearSheet("Stats!A2:I")
	updateSheet("Stats!A2:I", &vr)
}

func processDailyIssues(vr *sheets.ValueRange) {

	date := time.Now().Add(-time.Hour * 24).Format("2006-01-02")
	url := "https://github.com/istio/istio/issues?q=is%3Aissue+is%3Aopen+sort%3Aupdated-desc+created%3A+" + date
	vr.Values = append(vr.Values, []interface{}{"Istio daily Issues", getDailyIssues("istio"), url})
	vr.Values = append(vr.Values, []interface{}{"", ""})
	url = "https://github.com/istio/istio.io/issues?q=is%3Aissue+is%3Aopen+sort%3Aupdated-desc+created%3A+" + date
	vr.Values = append(vr.Values, []interface{}{"Docs daily Issues", getDailyIssues("istio.io"), url})
	vr.Values = append(vr.Values, []interface{}{"", ""})
}

func processPullRequests(vr *sheets.ValueRange) {
	getPullRequests("istio")
	for priority, issues := range priorities {
		vr.Values = append(vr.Values, []interface{}{fmt.Sprintf("%v issues", priority), len(issues)})
		vr.Values = append(vr.Values, []interface{}{})
	}

	var unreviewed, notMerged, changesRequested int
	for _, node := range allPullRequests {
		pr := node.Node
		switch pr.ReviewDecision {
		case githubv4.PullRequestReviewDecisionApproved:
			notMerged++
		case githubv4.PullRequestReviewDecisionReviewRequired:
			unreviewed++
		case githubv4.PullRequestReviewDecisionChangesRequested:
			changesRequested++
		}
	}
	reviewRequiredURL := "https://github.com/istio/istio/pulls?q=is%3Apr+is%3Aopen+sort%3Aupdated-desc+review%3Arequired"
	vr.Values = append(vr.Values, []interface{}{"Istio PRs waiting on review", unreviewed, reviewRequiredURL})
	vr.Values = append(vr.Values, []interface{}{})
	readyToMergeURL := "https://github.com/istio/istio/pulls?q=is%3Apr+is%3Aopen+sort%3Aupdated-desc+review%3Aapproved"
	vr.Values = append(vr.Values, []interface{}{"Istio PRs waiting to merge", notMerged, readyToMergeURL})
	vr.Values = append(vr.Values, []interface{}{"", ""})
}

func getDailyIssues(repo string) int {
	q := queries.DailyIssueQuery{}
	variables := map[string]interface{}{
		"date": githubv4.String(time.Now().Add(-time.Hour * 24).Format(time.RFC3339)),
		"name": githubv4.String(repo),
	}
	err := client.Query(context.Background(), &q, variables)
	if err != nil {
		fmt.Printf("%v\n", q)
		log.Fatalf("Query failed, err: %v", err)
	}
	fmt.Printf("retrieved daily issues for %v\n", repo)
	return len(q.Repository.Issues.Edges)
}

func getPullRequests(repo string) {
	q := queries.PullRequestsQuery{}
	variables := map[string]interface{}{
		"name": githubv4.String(repo),
	}
	err := client.Query(context.Background(), &q, variables)
	if err != nil {
		fmt.Printf("%v\n", q)
		log.Fatalf("Query failed, err: %v", err)
	}
	fmt.Printf("retrieved pull requests for %v\n", repo)
	allPullRequests = append(allPullRequests, q.Repository.PullRequests.Edges...)
}

func updateSheet(sheetRange string, vr *sheets.ValueRange) {
	var err error
	server, err = sheets.NewService(context.Background(), option.WithCredentialsFile(googleCreds), option.WithScopes(sheets.SpreadsheetsScope))

	if err != nil {
		log.Fatalf("Unable to retrieve Sheets Client %v", err)
	}

	_, err = server.Spreadsheets.Values.Append(sheetID, sheetRange, vr).ValueInputOption("RAW").InsertDataOption("OVERWRITE").Do()

	if err != nil {
		fmt.Println("Unable to update data to sheet  ", err)
	}

}

func clearSheet(sheetRange string) {
	var err error
	server, err = sheets.NewService(context.Background(), option.WithCredentialsFile(googleCreds), option.WithScopes(sheets.SpreadsheetsScope))

	if err != nil {
		log.Fatalf("Unable to retrieve Sheets Client %v", err)
	}
	rb := &sheets.ClearValuesRequest{
		// TODO: Add desired fields of the request body.
	}
	_, err = server.Spreadsheets.Values.Clear(sheetID, sheetRange, rb).Context(context.Background()).Do()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("cleared %v\n", sheetRange)
}
