package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/GregHanson/istio-stats/community-testing"
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
	githubClient    *githubv4.Client
	testStats       community.TestStats
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
	githubClient = githubv4.NewClient(httpClient)

	getIssues()
	stats()
}

func getIssues() {
	q := queries.MilestoneIssueQuery{}
	variables := map[string]interface{}{
		"commentsCursor": (*githubv4.String)(nil), // Null after argument to get first page.
		"name":           githubv4.String("istio"),
	}
	for {
		err := githubClient.Query(context.Background(), &q, variables)
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

	p := []githubv4.String{"Release  Blocker", "P0", "P1", "P2", "> P2"}
	for _, priority := range p {
		fmt.Printf("Priority {%v} has {%v} items OPEN in it\n", priority, len(priorities[priority]))
		for _, issue := range priorities[priority] {
			stale := ""
			if !issue.LastEditedAt.Time.Add(staleFactor).Before(time.Now()) {
				fmt.Printf("\tStale: %v\n", issue.Title)
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

func stats() {
	var vr sheets.ValueRange
	since := time.Now().Add(-time.Hour * 24 * 7)
	checkSignup()

	//processPullRequests(&vr)
	istioIssues, docsIssues := processIssuesHistory(since)

	vr.Values = append(vr.Values, []interface{}{since.Format("2006-01-02"),
		len(testStats.Participants),
		testStats.ClaimedTests,
		testStats.Priority0.Claimed,
		testStats.Priority0.Done,
		testStats.Priority0.Automated,
		testStats.Priority1.Claimed,
		testStats.Priority1.Done,
		testStats.Priority1.Automated,
		testStats.Priority2.Claimed,
		testStats.Priority2.Done,
		testStats.Priority2.Automated,
		(testStats.Priority0.Claimed + testStats.Priority1.Claimed + testStats.Priority2.Claimed) * 100 / testStats.Total,
		testStats.ClaimedTests * 100 / testStats.Total,
		istioIssues,
		docsIssues,
	})

	updateSheet("Stats!A:P", &vr)
}

func processIssuesHistory(since time.Time) (int, int) {
	return getDailyIssues("istio", since), getDailyIssues("istio.io", since)
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

func getDailyIssues(repo string, since time.Time) int {
	q := queries.HistoricalIssueQuery{}
	variables := map[string]interface{}{
		"date": githubv4.String(since.Format(time.RFC3339)),
		"name": githubv4.String(repo),
	}
	err := githubClient.Query(context.Background(), &q, variables)
	if err != nil {
		fmt.Printf("%v\n", q)
		log.Fatalf("Query failed, err: %v", err)
	}

	issuesSince := 0
	for _, edge := range q.Repository.Issues.Edges {
		if edge.Node.CreatedAt.Time.After(since) {
			issuesSince++
		}
	}

	fmt.Printf("retrieved daily issues for %v\n", repo)
	return issuesSince
}

func getPullRequests(repo string) {
	q := queries.PullRequestsQuery{}
	variables := map[string]interface{}{
		"name": githubv4.String(repo),
	}
	err := githubClient.Query(context.Background(), &q, variables)
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

func checkError(err error) {
	if err != nil {
		panic(err.Error())
	}
}

func checkSignup() {
	srv, err := sheets.NewService(context.Background(), option.WithCredentialsFile(googleCreds), option.WithScopes(sheets.SpreadsheetsScope))
	checkError(err)

	spreadsheetID := "1g6qsYnIkLHMXn210HkB3pJCGQMC4Adr7fqqlWLJ8uw4"
	readRange := "'Testing week 2'!B:H"
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetID, readRange).Do()
	checkError(err)

	docTests := []*community.DocTest{}

	if len(resp.Values) == 0 {
		fmt.Println("No data found.")
	} else {
		for rowIndex, row := range resp.Values {
			if rowIndex == 0 {
				continue
			}
			test := &community.DocTest{}
			for i := 0; i <= 5; i++ {
				if i >= len(row) {
					break
				}

				switch i {
				case 0:
					test.Doc = fmt.Sprintf("%v", row[i])
				case 1:
					test.Priority = fmt.Sprintf("%v", row[i])
				case 2:
					test.Automated = fmt.Sprintf("%v", row[i])
				case 3:
					test.Assigned = fmt.Sprintf("%v", row[i])
				case 4:
					test.Inprogress = fmt.Sprintf("%v", row[i])
				case 5:
					test.DoneBy = fmt.Sprintf("%v", row[i])
				}
			}
			if test.Automated != "" && test.Automated != "N/A" {
				docTests = append(docTests, test)
			}
		}
	}

	participants := map[string]int{}
	unclaimed := 0
	testStats = community.TestStats{}
	for _, test := range docTests {
		claimed := 0
		done := 0
		if test.Assigned != "" {
			participants[test.Assigned] = participants[test.Assigned] + 1
			claimed = 1
		}
		if test.DoneBy != "" {
			participants[test.DoneBy] = participants[test.DoneBy] + 1
			done = 1
		}
		if test.Inprogress != "" {
			participants[test.Inprogress] = participants[test.Inprogress] + 1
			claimed = 1
		}
		if test.Assigned == "" && test.Inprogress == "" && test.DoneBy == "" {
			unclaimed++
		}

		automated := 0
		if test.Automated == "DONE" {
			automated = 1
		}

		switch test.Priority {
		case "P0":
			testStats.Priority0.Automated += automated
			testStats.Priority0.Claimed += claimed
			testStats.Priority0.Done += done
			testStats.Priority0.Total++
		case "P1":
			testStats.Priority1.Automated += automated
			testStats.Priority1.Claimed += claimed
			testStats.Priority1.Done += done
			testStats.Priority1.Total++
		case "P2":
			testStats.Priority2.Automated += automated
			testStats.Priority2.Claimed += claimed
			testStats.Priority2.Done += done
			testStats.Priority2.Total++
		}
	}
	testStats.Participants = participants
	testStats.ClaimedTests = len(docTests) - unclaimed
	testStats.Total = len(docTests)
	fmt.Printf("%+v\n", testStats)
}
