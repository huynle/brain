package commands

// GoalFlags holds flags for the `brain automation goal` subcommands.
type GoalFlags struct {
	Project       string   // --project
	Feature       string   // --feature
	Title         string   // --title
	Content       string   // --content
	TriggerSource string   // --trigger-source (task|feature|both)
	SessionMode   string   // --session-mode (continue|fresh)
	Agent         string   // --agent
	Model         string   // --model
	Executor      string   // --executor
	Workdir       string   // --workdir
	Status        string   // --status
	Criteria      []string // --criteria (repeatable)
	Validate      []string // --validate (repeatable)
	Format        string   // --format (json|table)
	Limit         int      // --limit
	Quiet         bool     // -q, --quiet
}
