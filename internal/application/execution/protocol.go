package execution

import (
	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
	"strconv"
)

type Signal = executionrules.Signal

func attemptKey(t model.Task, suffix string) string {
	return "attempt-" + strconv.Itoa(t.Execution.Attempt) + "-" + suffix
}
