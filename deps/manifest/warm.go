package manifest

// WarmRequest describes one dependency to warm into the machine's shared
// package cache. Dir is a scratch project the orchestrator has already created;
// a Warmer only decides what to run inside it.
type WarmRequest struct {
	Dir     string
	Name    string
	Version string
	Build   bool
	Verify  bool
}

type StepKind string

const (
	StepExec   StepKind = "exec"
	StepWrite  StepKind = "write"
	StepRemove StepKind = "remove"
)

// Step is one unit of warming work. Warming is not purely exec: node ecosystems
// need a package.json written before installing, and need node_modules removed
// before an offline replay can prove anything.
type Step struct {
	Kind    StepKind
	Name    string
	Command Command // StepExec
	Path    string  // StepWrite, StepRemove — relative to WarmRequest.Dir
	Content []byte  // StepWrite
}

// Detail renders just the action, with no step name, for callers that already
// report the name separately.
func (s Step) Detail() string {
	switch s.Kind {
	case StepExec:
		return s.Command.String()
	case StepWrite:
		return "write " + s.Path
	case StepRemove:
		return "remove " + s.Path
	default:
		return "unknown step kind " + string(s.Kind)
	}
}

// String renders the step as "name: kind detail", so a failure can name what was
// being attempted and tests can assert a whole sequence in one comparison.
func (s Step) String() string {
	if s.Kind == StepExec {
		return s.Name + ": exec " + s.Detail()
	}
	return s.Name + ": " + s.Detail()
}

// FormatSteps renders a sequence one line per step.
func FormatSteps(steps []Step) []string {
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		out = append(out, step.String())
	}
	return out
}

// Warmer decides which commands warm one ecosystem. Implementations do no I/O:
// Steps is a pure function of its arguments, which is what lets the per-manager
// packages be tested by comparing argv without a toolchain, a network, or a
// process.
//
// Probe is the escape hatch for a manager that genuinely needs runtime
// information (pnpm's lifecycle-script policy changed in 10.6). The orchestrator
// runs it and feeds the trimmed stdout back into Steps as an ordinary input,
// keeping Steps pure.
type Warmer interface {
	Manager() Manager
	// Binary is the executable that must be on PATH, so the orchestrator can
	// fail before it creates a scratch directory.
	Binary() string
	Probe() *Command
	Steps(req WarmRequest, probe string) ([]Step, error)
}
