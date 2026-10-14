package deps

import (
	"errors"
	"fmt"
	"strings"

	"github.com/flanksource/clicky/api"
)

type UpdateError struct {
	api.TextTable
	Plans []UpdatePlan `json:"plans"`
	cause error
}

func (e *UpdateError) Error() string { return e.cause.Error() }
func (e *UpdateError) Unwrap() error { return e.cause }

func finishUpdate(plans []UpdatePlan) ([]UpdatePlan, error) {
	var failures []error
	for _, plan := range plans {
		if plan.Error != "" {
			failures = append(failures, fmt.Errorf("%s (%s): %s", plan.Name, plan.File, plan.Error))
		}
		if plan.StageError != "" {
			failures = append(failures, fmt.Errorf("%s (%s): staging: %s", plan.Name, plan.File, plan.StageError))
		}
	}
	if len(failures) == 0 {
		return plans, nil
	}
	return plans, &UpdateError{TextTable: api.NewTableFrom(plans), Plans: plans, cause: errors.Join(failures...)}
}

func commandFailure(cmd Command, result CommandResult, err error) string {
	return fmt.Sprintf("%s (cwd %s): %v: %s", cmd.String(), cmd.Dir, err, strings.TrimSpace(result.Stderr+"\n"+result.Stdout))
}
