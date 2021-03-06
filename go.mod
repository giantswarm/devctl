module github.com/giantswarm/devctl

go 1.16

require (
	github.com/Masterminds/semver/v3 v3.1.1
	github.com/bmatcuk/doublestar v1.3.4
	github.com/giantswarm/apiextensions/v2 v2.6.2
	github.com/giantswarm/k8sclient/v4 v4.1.0
	github.com/giantswarm/kubeconfig/v2 v2.0.0
	github.com/giantswarm/microerror v0.3.0
	github.com/giantswarm/micrologger v0.5.0
	github.com/google/go-github v17.0.0+incompatible
	github.com/pelletier/go-toml v1.8.1
	github.com/prometheus/client_golang v1.7.1 // indirect
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	golang.org/x/net v0.0.0-20200520004742-59133d7f0dd7
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	k8s.io/apimachinery v0.18.5
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/coreos/etcd v3.3.10+incompatible => github.com/coreos/etcd v3.3.25+incompatible
	github.com/coreos/etcd v3.3.13+incompatible => github.com/coreos/etcd v3.3.25+incompatible
)
