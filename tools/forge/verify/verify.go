package verify

import (
	"fmt"
	"strings"
)

type Error struct {
	Stage   string
	Message string
	Details []string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s", e.Stage, e.Message)
	for _, d := range e.Details {
		fmt.Fprintf(&b, "\n  - %s", d)
	}
	return b.String()
}

func NewError(stage, message string, details ...string) *Error {
	return &Error{Stage: stage, Message: message, Details: details}
}

func Fail(stage, format string, args ...interface{}) *Error {
	return &Error{Stage: stage, Message: fmt.Sprintf(format, args...)}
}
