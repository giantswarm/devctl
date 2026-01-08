package params

import (
	"fmt"

	"github.com/giantswarm/devctl/v7/pkg/gen/internal"
)

func RegenerableFileName(p Params, suffix string) string {
	return internal.RegenerableFileName(p.Dir, suffix)
}

// RegenerableFolderRuleFileName returns the path for a RULE.md file in a
// folder-based Cursor rule structure. The folder name is prefixed with
// "zz_generated." to denote it can be regenerated.
func RegenerableFolderRuleFileName(p Params, ruleName string) string {
	return internal.RegenerableFolderRuleFileName(p.Dir, ruleName)
}

func IsLanguageGo(p Params) bool {
	return p.Language == "go"
}

func Header(githubUrl string) string {
	return fmt.Sprintf(`
<!--
DO NOT EDIT. Generated with devctl.
This file is maintained at:
%s
Manual changes will be overwritten.
-->`, githubUrl)
}
