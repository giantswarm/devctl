package create

import (
	"context"
	"fmt"
	"time"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// validationTimeout bounds the manager's schema, membership and name reads.
const validationTimeout = 2 * time.Minute

// managerValidation includes the planned pull request's refusal: an
// unreadable team file or an existing declaration can prevent creation
// even when the engine accepts the entry.
type managerValidation struct {
	reposetup.Result
	Creation struct {
		Refusal string `json:"refusal"`
	} `json:"creation"`
}

// manager asks for the creation preview without reading GitHub or running
// the validator locally. validate_repository shares create_repository's
// arguments and dry run; neither a missing login nor a failed call falls
// back to devctl's engine.
func (r *runner) manager(ctx context.Context) error {
	session, err := r.open(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	declaration, err := reposetup.Creation{
		Name:          r.flag.Name,
		ComponentType: r.flag.ComponentType,
		Description:   r.flag.Description,
		Visibility:    r.flag.Visibility,
		Flavours:      r.flag.Flavours,
		Language:      r.flag.Language,
	}.Declaration()
	if err != nil {
		return microerror.Mask(err)
	}
	entry, err := declaration.Instance()
	if err != nil {
		return microerror.Mask(err)
	}

	callCtx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()
	payload, err := session.Call(callCtx, manager.ToolValidateRepository, map[string]any{"team": r.flag.Team, "entry": entry})
	if err != nil {
		return microerror.Mask(err)
	}
	result, err := client.Decode[managerValidation](manager.ToolValidateRepository, payload)
	if err != nil {
		return microerror.Mask(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Name != r.flag.Name {
		return microerror.Maskf(client.UnexpectedAnswerError, "%s: expected the dry run of %s", manager.ToolValidateRepository, r.flag.Name)
	}
	if r.flag.Output == outputText {
		fmt.Fprintln(r.stdout, "validation: giantswarm-repo-manager")
		if err := r.printDryRun(&result.Result); err != nil {
			return microerror.Mask(err)
		}
		if result.Creation.Refusal != "" {
			fmt.Fprintf(r.stdout, "creation refused: %s\n", result.Creation.Refusal)
		}
		fmt.Fprintln(r.stdout, "dry run: no pull request opened")
	}
	// Keep the manager's fields, including ones this devctl does not know.
	r.print(output{DryRun: payload})
	if !result.Accepted || result.Creation.Refusal != "" {
		return microerror.Maskf(refusedError, "%s is refused by giantswarm-repo-manager; no pull request was opened", r.flag.Name)
	}
	return nil
}
