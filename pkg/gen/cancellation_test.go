package gen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

func Test_Execute_CanceledBeforeMutation(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		name := "generate"
		if deletion {
			name = "delete"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			destination := filepath.Join(dir, "nested", "zz_generated.fixture")
			if deletion {
				destination = filepath.Join(dir, "legacy.yaml")
				if err := os.WriteFile(destination, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			called := false
			err := Execute(ctx, input.Input{
				Path: destination, Delete: deletion,
				Generate: func(context.Context) ([]byte, error) {
					called = true
					return []byte("replacement"), nil
				},
			})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("got %v, want context.Canceled", err)
			}
			if called {
				t.Error("started generation after cancellation")
			}
			if deletion {
				data, err := os.ReadFile(destination)
				if err != nil || string(data) != "original" {
					t.Errorf("canceled deletion changed the file: %q, %v", data, err)
				}
			} else if _, err := os.Stat(filepath.Dir(destination)); !os.IsNotExist(err) {
				t.Errorf("canceled generation created its directory: %v", err)
			}
		})
	}
}

func Test_Execute_CanceledDuringGeneration(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "values.schema.json")
	if err := os.WriteFile(destination, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calledNext := false
	err := Execute(ctx,
		input.Input{
			Path: destination,
			Generate: func(got context.Context) ([]byte, error) {
				if got != ctx {
					t.Error("Generate did not receive the caller context")
				}
				cancel()
				return []byte("replacement"), nil
			},
		},
		input.Input{
			Path: filepath.Join(dir, "zz_generated.next"),
			Generate: func(context.Context) ([]byte, error) {
				calledNext = true
				return []byte("next"), nil
			},
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
	data, readErr := os.ReadFile(destination)
	if readErr != nil || string(data) != "original" {
		t.Errorf("published output after cancellation: %q, %v", data, readErr)
	}
	if calledNext {
		t.Error("started the next file after cancellation")
	}
}

func Test_Execute_ExpiredDeadline(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "zz_generated.fixture")
	ctx, cancel := context.WithDeadline(t.Context(), time.Unix(0, 0))
	defer cancel()
	if err := Execute(ctx, input.Input{Path: destination, TemplateBody: "fixture"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Errorf("expired call wrote output: %v", err)
	}
}
