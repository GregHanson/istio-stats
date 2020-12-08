package community

type DocTest struct {
	Doc        string
	Priority   string
	Automated  string
	Assigned   string
	Inprogress string
	DoneBy     string
}

type Priority struct {
	Claimed   int
	Done      int
	Automated int
	Total     int
}

type TestStats struct {
	Participants map[string]int
	ClaimedTests int
	Total        int
	Priority0    Priority
	Priority1    Priority
	Priority2    Priority
}
