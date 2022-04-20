module github.com/giantswarm/devctl

go 1.16

require (
	github.com/Masterminds/semver/v3 v3.1.1
	github.com/blang/semver v3.5.1+incompatible
	github.com/bmatcuk/doublestar v1.3.4
	github.com/fatih/color v1.13.0
	github.com/giantswarm/k8sclient/v7 v7.0.1
	github.com/giantswarm/kubeconfig/v4 v4.1.0
	github.com/giantswarm/microerror v0.4.0
	github.com/giantswarm/micrologger v0.6.0
	github.com/giantswarm/release-operator/v3 v3.2.0
	github.com/google/go-github v17.0.0+incompatible
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/pelletier/go-toml v1.9.4
	github.com/rhysd/go-github-selfupdate v1.2.3
	github.com/spf13/cobra v1.4.0
	github.com/spf13/pflag v1.0.5
	golang.org/x/net v0.0.0-20210917221730-978cfadd31cf
	golang.org/x/oauth2 v0.0.0-20211104180415-d3ed0bb246c8
	golang.org/x/sys v0.0.0-20211205182925-97ca703d548d // indirect
	k8s.io/apimachinery v0.20.12
	sigs.k8s.io/yaml v1.3.0
)

replace (
	github.com/coreos/etcd v3.3.13+incompatible => github.com/coreos/etcd v3.3.25+incompatible
	github.com/dgrijalva/jwt-go => github.com/dgrijalva/jwt-go/v4 v4.0.0-preview1

	// Mitigate a security issue in github.com/gorilla/websocket v1.4.0 and earlier
	github.com/gorilla/websocket => github.com/gorilla/websocket v1.4.2
)
