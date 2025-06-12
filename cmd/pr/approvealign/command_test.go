package approvealign

import (
	"bytes"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestNew(t *testing.T) {
	testCases := []struct {
		name        string
		config      Config
		expectError bool
	}{
		{
			name: "valid config",
			config: Config{
				Logger: logrus.New(),
				Stderr: &bytes.Buffer{},
				Stdout: &bytes.Buffer{},
			},
			expectError: false,
		},
		{
			name: "nil logger",
			config: Config{
				Logger: nil,
				Stderr: &bytes.Buffer{},
				Stdout: &bytes.Buffer{},
			},
			expectError: true,
		},
		{
			name: "nil stderr and stdout (should use defaults)",
			config: Config{
				Logger: logrus.New(),
				Stderr: nil,
				Stdout: nil,
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := New(tc.config)

			if tc.expectError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if cmd == nil {
				t.Fatal("Expected command but got nil")
			}

			// Test command properties
			if cmd.Use != longCmd {
				t.Fatalf("Expected Use to be %q, got %q", longCmd, cmd.Use)
			}

			if cmd.Short != description {
				t.Fatalf("Expected Short to be %q, got %q", description, cmd.Short)
			}

			if cmd.Long != description {
				t.Fatalf("Expected Long to be %q, got %q", description, cmd.Long)
			}

			// Check aliases
			expectedAliases := []string{name, shortCmd}
			if len(cmd.Aliases) != len(expectedAliases) {
				t.Fatalf("Expected %d aliases, got %d", len(expectedAliases), len(cmd.Aliases))
			}

			for i, alias := range expectedAliases {
				if cmd.Aliases[i] != alias {
					t.Fatalf("Expected alias %d to be %q, got %q", i, alias, cmd.Aliases[i])
				}
			}

			// Test that RunE is set
			if cmd.RunE == nil {
				t.Fatal("Expected RunE to be set")
			}
		})
	}
}

func TestCommandConstants(t *testing.T) {
	// Test that constants are set correctly
	if name != "approvealign" {
		t.Fatalf("Expected name to be 'approvealign', got %q", name)
	}

	if shortCmd != "approvealignfiles" {
		t.Fatalf("Expected shortCmd to be 'approvealignfiles', got %q", shortCmd)
	}

	if longCmd != "approve-align-files" {
		t.Fatalf("Expected longCmd to be 'approve-align-files', got %q", longCmd)
	}

	if description != "Approves 'Align files' PRs with passing status checks." {
		t.Fatalf("Expected description to match, got %q", description)
	}
}
