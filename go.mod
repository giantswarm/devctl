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
	github.com/google/go-github/v44 v44.1.0
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/pelletier/go-toml v1.9.5
	github.com/rhysd/go-github-selfupdate v1.2.3
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.4.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.2 // indirect
	golang.org/x/crypto v0.0.0-20220525230936-793ad666bf5e // indirect
	golang.org/x/net v0.0.0-20220607020251-c690dde0001d
	golang.org/x/oauth2 v0.0.0-20211104180415-d3ed0bb246c8
	golang.org/x/time v0.0.0-20220411224347-583f2d630306 // indirect
	k8s.io/apimachinery v0.20.12
	sigs.k8s.io/yaml v1.3.0
)

replace (
	github.com/aws/aws-sdk-go => github.com/aws/aws-sdk-go v1.44.29
	github.com/coreos/etcd v3.3.13+incompatible => github.com/coreos/etcd v3.3.25+incompatible
	github.com/dgrijalva/jwt-go => github.com/dgrijalva/jwt-go/v4 v4.0.0-preview1
	github.com/gin-gonic/gin => github.com/gin-gonic/gin v1.8.1
	github.com/go-ldap/ldap/v3 => github.com/go-ldap/ldap/v3 v3.4.3

	// Mitigate a security issue in github.com/gorilla/websocket v1.4.0 and earlier
	github.com/gorilla/websocket => github.com/gorilla/websocket v1.4.2

	// Mitigate security issues
	github.com/hashicorp/consul/api => github.com/hashicorp/consul/api v1.13.0
	github.com/hashicorp/consul/sdk => github.com/hashicorp/consul/sdk v0.9.0
	github.com/hashicorp/vault/api => github.com/hashicorp/vault/api v1.6.0
	github.com/pkg/sftp => github.com/pkg/sftp v1.13.4
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v1.12.2
	github.com/valyala/fasthttp => github.com/valyala/fasthttp v1.37.0

)
