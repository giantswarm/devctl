package resource

import (
	"fmt"
	"path"
)

func clientImport(objectGroup string) string {
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

func clientPackage(objectGroup string) string {
	return path.Base(clientImport(objectGroup))
}

func objectImport(objectGroup, objectVersion string) string {
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

/* `package` would be a better name but it's a keyword. */
func packageName(dir string) string {
	return path.Base(dir)
}
