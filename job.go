package atc

type Job struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	Paused        bool   `json:"paused,omitempty"`
	NextBuild     *Build `json:"next_build"`
	FinishedBuild *Build `json:"finished_build"`

	Inputs  []JobInput  `json:"inputs"`
	Outputs []JobOutput `json:"outputs"`

	Groups []string `json:"groups"`
}

type JobInput struct {
	Name     string   `json:"name"`
	Resource string   `json:"resource"`
	Passed   []string `json:"passed,omitempty"`
	Trigger  bool     `json:"trigger"`
}

type JobOutput struct {
	Name     string `json:"name"`
	Resource string `json:"resource"`
}

type BuildInput struct {
	Name     string   `json:"name"`
	Resource string   `json:"resource"`
	Type     string   `json:"type"`
	Source   Source   `json:"source"`
	Params   Params   `json:"params,omitempty"`
	Version  Version  `json:"version"`
	Tags     []string `json:"tags,omitempty"`
}
