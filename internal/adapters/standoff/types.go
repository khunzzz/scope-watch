package standoff

// Private wire types describe this platform's API, independently of scope.Program.
type programData struct {
	Slug         string  `json:"slug"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	SpecialRules string  `json:"specialRules"`
	Status       string  `json:"status"`
	Visibility   string  `json:"visibility"`
	UpdatedAt    string  `json:"updatedAt"`
	Finished     bool    `json:"finished"`
	ArchivedAt   *string `json:"archivedAt"`
}

type rewardData struct {
	MinReward int `json:"minReward"`
	MaxReward int `json:"maxReward"`
}

type page struct {
	Programs []programData
	Total    *int
}
