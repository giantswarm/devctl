package params

import (
	"fmt"
	"path"

	"github.com/giantswarm/devctl/pkg/gen/internal"
)

func ClientImport(objectGroup string) string {
	switch objectGroup {
	case "core":
		return "k8s.io/client-go/kubernetes"
	case "g8s":
		return "github.com/giantswarm/apiextensions/pkg/clientset/versioned"
	default:
		// This is validated in Config.Validate. If this happens then
		// this is a bug in the code.
		panic(fmt.Sprintf("determine client import for group %#q", objectGroup))
	}
}

func ClientPackage(objectGroup string) string {
	return path.Base(ClientImport(objectGroup))
}

func ObjectImport(objectGroup, objectVersion string) string {
	switch objectGroup {
	case "core":
		return "k8s.io/api/" + objectGroup + "/" + objectVersion
	case "g8s":
		return "github.com/giantswarm/apiextensions/pkg/apis/" + objectGroup + "/" + objectVersion
	default:
		// This is validated in Config.Validate. If this happens then
		// this is a bug in the code.
		panic(fmt.Sprintf("determine object import for group %#q", objectGroup))
	}
}

func Package(dir string) string {
	return path.Base(dir)
}

func RegenerableFileName(suffix string) string {
	return internal.RegenerableFilePrefix + suffix
}
