package engine

import "fmt"

// Sentinel error types for categorizing failures across the engine.
// Use errors.As in cmd/ to differentiate exit codes or user messages.

type ValidationError struct {
	Msg string
	Err error
}

func (e *ValidationError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("validation: %s: %v", e.Msg, e.Err)
	}
	return fmt.Sprintf("validation: %s", e.Msg)
}

func (e *ValidationError) Unwrap() error { return e.Err }

type GitError struct {
	Msg string
	Err error
}

func (e *GitError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("git: %s: %v", e.Msg, e.Err)
	}
	return fmt.Sprintf("git: %s", e.Msg)
}

func (e *GitError) Unwrap() error { return e.Err }

type ReasoningError struct {
	Msg string
	Err error
}

func (e *ReasoningError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("reasoning: %s: %v", e.Msg, e.Err)
	}
	return fmt.Sprintf("reasoning: %s", e.Msg)
}

func (e *ReasoningError) Unwrap() error { return e.Err }

func NewValidationError(msg string, err error) error {
	return &ValidationError{Msg: msg, Err: err}
}

func NewGitError(msg string, err error) error {
	return &GitError{Msg: msg, Err: err}
}

func NewReasoningError(msg string, err error) error {
	return &ReasoningError{Msg: msg, Err: err}
}
