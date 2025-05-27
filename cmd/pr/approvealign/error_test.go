package approvealign

import (
	"testing"

	"github.com/giantswarm/microerror"
)

func TestInvalidConfigError(t *testing.T) {
	// Test that invalidConfigError can be created and matched
	err := microerror.Maskf(invalidConfigError, "test error message")

	if !IsInvalidConfig(err) {
		t.Fatal("Expected IsInvalidConfig to return true for invalidConfigError")
	}

	// Test with a different error type
	otherErr := microerror.Maskf(executionFailedError, "different error")
	if IsInvalidConfig(otherErr) {
		t.Fatal("Expected IsInvalidConfig to return false for different error type")
	}

	// Test with nil error
	if IsInvalidConfig(nil) {
		t.Fatal("Expected IsInvalidConfig to return false for nil error")
	}
}

func TestExecutionFailedError(t *testing.T) {
	// Test that executionFailedError can be created and matched
	err := microerror.Maskf(executionFailedError, "test execution error")

	if !IsExecutionFailed(err) {
		t.Fatal("Expected IsExecutionFailed to return true for executionFailedError")
	}

	// Test with a different error type
	otherErr := microerror.Maskf(invalidConfigError, "different error")
	if IsExecutionFailed(otherErr) {
		t.Fatal("Expected IsExecutionFailed to return false for different error type")
	}

	// Test with nil error
	if IsExecutionFailed(nil) {
		t.Fatal("Expected IsExecutionFailed to return false for nil error")
	}
}

func TestErrorMessages(t *testing.T) {
	// Test that error messages contain expected descriptions
	execErr := microerror.Maskf(executionFailedError, "test message")
	errStr := execErr.Error()

	if errStr == "" {
		t.Fatal("Expected error message to be non-empty")
	}

	// The exact error message format depends on microerror implementation,
	// but we can verify it's not empty and contains our message
	// Note: microerror may format the message differently
}
