package bizone

// Private wire types describe this platform's API, independently of scope.Program.
type programData struct {
	Slug                  string `json:"slug"`
	Name                  string `json:"name"`
	Description           string `json:"description"`
	Rules                 string `json:"rules"`
	Public                *bool  `json:"public"`
	IsActive              *bool  `json:"isActive"`
	MinLowPrice           int    `json:"minLowPrice"`
	MaxLowPrice           int    `json:"maxLowPrice"`
	MinMediumPrice        int    `json:"minMediumPrice"`
	MaxMediumPrice        int    `json:"maxMediumPrice"`
	MinHighPrice          int    `json:"minHighPrice"`
	MaxHighPrice          int    `json:"maxHighPrice"`
	MinCriticalPrice      int    `json:"minCriticalPrice"`
	MaxCriticalPrice      int    `json:"maxCriticalPrice"`
	ModificationDateRules string `json:"modificationDateRules"`
	ModificationDatePrice string `json:"modificationDatePrice"`
}

type taskData struct {
	ID          int    `json:"id"`
	TextTask    string `json:"textTask"`
	Description string `json:"description"`
	InScope     *bool  `json:"inScope"`
	Eligibility *bool  `json:"eligibility"`
	InArchive   bool   `json:"inArchive"`
	Company     *struct {
		Slug string `json:"slug"`
	} `json:"company"`
}

type versionData struct {
	Type    string `json:"type"`
	Version any    `json:"version"`
}
