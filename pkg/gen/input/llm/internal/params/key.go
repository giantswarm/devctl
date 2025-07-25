package params

import (
	"fmt"

	"github.com/giantswarm/devctl/v7/pkg/gen/internal"
)

func RegenerableFileName(p Params, suffix string) string {
	return internal.RegenerableFileName(p.Dir, suffix)
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
