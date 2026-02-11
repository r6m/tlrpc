package codegen

import "fmt"

// ParseError represents a parsing error with position information.
type ParseError struct {
	Line    int
	Column int
	Message string
	Token   Token // The token that caused the error
}

func (e ParseError) Error() string {
	return fmt.Sprintf("parse error at %d:%d: %s (near %s)", e.Line, e.Column, e.Message, e.Token.Literal)
}

// ErrorSeverity represents the severity of a validation error.
type ErrorSeverity int

const (
	ErrorSeverityWarning ErrorSeverity = iota
	ErrorSeverityError
)

// String returns a string representation of the error severity.
func (s ErrorSeverity) String() string {
	switch s {
	case ErrorSeverityWarning:
		return "warning"
	case ErrorSeverityError:
		return "error"
	default:
		return "unknown"
	}
}

// ValidationError represents a validation error.
type ValidationError struct {
	Line     int
	Column  int
	Message string
	Severity ErrorSeverity
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s at %d:%d: %s", e.Severity.String(), e.Line, e.Column, e.Message)
}

// MultiError represents multiple errors.
type MultiError struct {
	Errors []error
}

func (e MultiError) Error() string {
	if len(e.Errors) == 0 {
		return "no errors"
	}
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}

	result := fmt.Sprintf("%d errors:", len(e.Errors))
	for i, err := range e.Errors {
		result += fmt.Sprintf("\n  %d. %s", i+1, err.Error())
	}
	return result
}

// NewParseError creates a new parse error.
func NewParseError(line, col int, message string, token Token) *ParseError {
	return &ParseError{
		Line:     line,
		Column:  col,
		Message: message,
		Token:   token,
	}
}

// NewValidationError creates a new validation error.
func NewValidationError(line, col int, message string, severity ErrorSeverity) *ValidationError {
	return &ValidationError{
		Line:     line,
		Column:  col,
		Message: message,
		Severity: severity,
	}
}

// NewMultiError creates a new multi-error from a slice of errors.
func NewMultiError(errors []error) *MultiError {
	return &MultiError{Errors: errors}
}