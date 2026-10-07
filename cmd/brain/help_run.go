package main

// runSubcommandHelp holds the per-subcommand pages for `brain run <sub>`
// (also reachable as `brain runner <sub>`). `run start` has its own page in
// help.go.
var runSubcommandHelp = map[string]string{
	"run status": `brain run status - Show task pause state and registered runners

USAGE:
  brain run status

Prints whether task dispatch is paused (and for which projects) on the
configured Brain API, then lists the runners registered with it.
`,
	"run list": `brain run list - List projects or a project's tasks

USAGE:
  brain run list            Projects with task, ready and blocked counts
  brain run list <project>  Tasks of one project
`,
	"run ready": `brain run ready - List a project's ready tasks

USAGE:
  brain run ready <project>
`,
	"run features": `brain run features - List a project's features

USAGE:
  brain run features <project>

Columns: TASKS, COMPLETED, READY (runnable now), WAITING (dependencies still in
progress), BLOCKED (unmet or blocked dependencies) and STARTABLE (the feature's
own dependencies are satisfied). Unknown feature dependencies are listed as
warnings.
`,
	"run logs": `brain run logs - Show a task's latest log lines

USAGE:
  brain run logs <project> <taskId> [--limit N]

FLAGS:
  --limit N                      Lines to show (server default 100, max 1000)

The logs endpoint does not stream, so this is one-shot; -f/--follow prints a
notice and shows the latest lines once.
`,
	"run config": `brain run config - Show the server's task defaults

USAGE:
  brain run config

Prints GET /api/v1/config/task-defaults as KEY/VALUE ("-" means unset).
`,
	"run pause": `brain run pause - Pause task dispatch for one project

USAGE:
  brain run pause <project>

Runners stop claiming that project's tasks; running tasks are not killed and
no runner process is stopped. Undo with 'brain run resume <project>'.
To pause every project, use 'brain run pause-all'.
`,
	"run resume": `brain run resume - Resume task dispatch for one project

USAGE:
  brain run resume <project>
`,
	"run pause-all": `brain run pause-all - Pause task dispatch for ALL projects (server-wide)

USAGE:
  brain run pause-all [--yes]

Affects every project and every runner on the configured Brain API. Shows how
many projects will be paused and asks y/N. Takes no project argument (use
'brain run pause <project>' for one).

FLAGS:
  -y, --yes                      Skip the prompt; required when stdin is not a
                                 terminal (otherwise it refuses)
`,
	"run resume-all": `brain run resume-all - Resume task dispatch for ALL projects

USAGE:
  brain run resume-all [--yes]

Lists the currently paused projects and asks y/N. This also resumes projects
that were paused individually; to resume one, use 'brain run resume <project>'.

FLAGS:
  -y, --yes                      Skip the prompt; required when stdin is not a
                                 terminal (otherwise it refuses)
`,
	"run stop": `brain run stop - Renamed to 'brain run pause-all'

'brain run stop' paused every project server-wide and never stopped the local
runner. It now exits with an error.

  Pause all projects:            brain run pause-all
  Stop a background runner:      brain runner stop [-n <name>|--all]
  Stop a foreground runner:      stop its process (Ctrl+C)
`,
}
