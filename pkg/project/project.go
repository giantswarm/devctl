package project

var (
	description = "Command line tool for the development daily business."
	gitSHA      = "n/a"
	name        = "devctl"
	source      = "https://github.com/giantswarm/devctl"
	version     = "5.11.1"
)

func Description() string {
	return description
}

func GitSHA() string {
	return gitSHA
}

func Name() string {
	return name
}

func Source() string {
	return source
}

func Version() string {
	return version
}
