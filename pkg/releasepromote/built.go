package releasepromote

import (
	"fmt"

	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// verifiedTagPipeline refuses a fallback verdict from statuses of a shared
// commit. Only a pipeline resolved by the requested tag proves its build ran.
func verifiedTagPipeline(result releasewait.Result) error {
	if result.CIModel == releasewait.CIModelGenerated || result.CIModel == releasewait.CIModelHandWritten {
		if result.Pipeline == nil || result.Pipeline.ID == "" {
			return fmt.Errorf("no verified CircleCI tag pipeline for %s; commit statuses alone do not identify the tag's build", result.Tag)
		}
	}
	return nil
}
