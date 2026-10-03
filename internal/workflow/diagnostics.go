package workflow

import (
	"errors"
	"strconv"
	"strings"

	"apihub-go/internal/executor"
	"apihub-go/internal/jsonutil"
)

// DiagnosticError keeps a source location attached while callers add context.
type DiagnosticError struct {
	Cause      error
	Diagnostic Diagnostic
}

func (e *DiagnosticError) Error() string { return e.Cause.Error() }
func (e *DiagnosticError) Unwrap() error { return e.Cause }
func Located(err error, code, stepID, phase, path string) error {
	if err == nil {
		return nil
	}
	var located *DiagnosticError
	if errors.As(err, &located) {
		return err
	}
	return &DiagnosticError{Cause: err, Diagnostic: Diagnostic{Code: code, StepID: stepID, Phase: phase, FieldPath: path, Severity: "error", Message: code}}
}
func Diagnostics(err error) []Diagnostic {
	var located *DiagnosticError
	if errors.As(err, &located) {
		return []Diagnostic{located.Diagnostic}
	}
	var code *CodeError
	if errors.As(err, &code) {
		return code.Diagnostics
	}
	return nil
}
func FieldDiagnostic(err error, stepID, phase, prefix string) []Diagnostic {
	var field *executor.FieldError
	if !errors.As(err, &field) {
		return Diagnostics(err)
	}
	path := prefix
	escape := func(key string) string { return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1") }
	if field.Path != "" && !strings.HasPrefix(field.Path, "$") {
		path += "/" + escape(field.Path)
	} else if parts, ok := jsonutil.PathSegments("root" + strings.TrimPrefix(field.Path, "$")); ok {
		for _, part := range parts[1:] {
			if !part.IsIndex {
				path += "/" + escape(part.Key)
			} else {
				path += "/" + strconv.Itoa(part.Index)
			}
		}
	}
	if field.Constraint == "required" && field.Expected != "" && !strings.Contains(field.Expected, ", ") {
		path += "/" + escape(field.Expected)
	}
	return []Diagnostic{{Code: "invalid_parameter", StepID: stepID, Phase: phase, FieldPath: path, Severity: "error", Message: field.Constraint}}
}
