module github.com/giantswarm/devctl

go 1.18

require (
	github.com/Masterminds/semver/v3 v3.2.0
	github.com/blang/semver v3.5.1+incompatible
	github.com/bmatcuk/doublestar v1.3.4
	github.com/fatih/color v1.15.0
	github.com/giantswarm/microerror v0.4.0
	github.com/giantswarm/micrologger v0.6.0
	github.com/giantswarm/release-operator/v3 v3.2.0
	github.com/google/go-github/v44 v44.1.0
	github.com/pelletier/go-toml v1.9.5
	github.com/rhysd/go-github-selfupdate v1.2.3
	github.com/sirupsen/logrus v1.9.0
	github.com/spf13/cobra v1.6.1
	github.com/spf13/pflag v1.0.5
	golang.org/x/net v0.9.0
	golang.org/x/oauth2 v0.7.0
	k8s.io/apimachinery v0.20.12
	sigs.k8s.io/yaml v1.3.0
)

require (
	github.com/giantswarm/k8smetadata v0.6.0 // indirect
	github.com/go-kit/log v0.2.0 // indirect
	github.com/go-logfmt/logfmt v0.5.1 // indirect
	github.com/go-logr/logr v0.4.0 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-github/v30 v30.1.0 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/inconshreveable/go-update v0.0.0-20160112193335-8152e7eb6ccf // indirect
	github.com/inconshreveable/mousetrap v1.0.1 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kr/pretty v0.2.1 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.17 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/onsi/gomega v1.10.2 // indirect
	github.com/stretchr/testify v1.7.2 // indirect
	github.com/tcnksm/go-gitconfig v0.1.2 // indirect
	github.com/ulikunitz/xz v0.5.9 // indirect
	golang.org/x/crypto v0.5.0 // indirect
	golang.org/x/sys v0.7.0 // indirect
	golang.org/x/text v0.9.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/klog/v2 v2.9.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.1.2 // indirect
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
	github.com/hashicorp/consul/api => github.com/hashicorp/consul/api v1.15.2
	github.com/hashicorp/consul/sdk => github.com/hashicorp/consul/sdk v0.11.0
	github.com/hashicorp/vault/api => github.com/hashicorp/vault/api v1.6.0

	// Fix for CWE-121: Stack-based Buffer Overflow
	github.com/nats-io/nats-server/v2 => github.com/nats-io/nats-server/v2 v2.9.11

	github.com/pkg/sftp => github.com/pkg/sftp v1.13.4
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v1.12.2
	github.com/valyala/fasthttp => github.com/valyala/fasthttp v1.37.0

	// Fix for CWE-400: Uncontrolled Resource Consumption ('Resource Exhaustion')
	golang.org/x/text => golang.org/x/text v0.3.8

)
