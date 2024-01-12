module github.com/giantswarm/devctl/v6

go 1.21

toolchain go1.21.5

require (
	github.com/Masterminds/semver/v3 v3.2.1
	github.com/aws/aws-sdk-go v1.49.19
	github.com/blang/semver v3.5.1+incompatible
	github.com/bmatcuk/doublestar/v4 v4.6.1
	github.com/fatih/color v1.16.0
	github.com/giantswarm/microerror v0.4.1
	github.com/giantswarm/micrologger v1.1.1
	github.com/giantswarm/release-operator/v4 v4.1.1
	github.com/google/go-github/v58 v58.0.0
	github.com/pelletier/go-toml/v2 v2.1.1
	github.com/rhysd/go-github-selfupdate v1.2.3
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/cobra v1.8.0
	github.com/spf13/pflag v1.0.5
	golang.org/x/net v0.20.0
	golang.org/x/oauth2 v0.16.0
	k8s.io/apimachinery v0.28.4
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/giantswarm/k8smetadata v0.23.0 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/go-logr/logr v1.3.0 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/go-github/v30 v30.1.0 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/inconshreveable/go-update v0.0.0-20160112193335-8152e7eb6ccf // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/onsi/gomega v1.10.2 // indirect
	github.com/tcnksm/go-gitconfig v0.1.2 // indirect
	github.com/ulikunitz/xz v0.5.9 // indirect
	golang.org/x/crypto v0.18.0 // indirect
	golang.org/x/sys v0.16.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/klog/v2 v2.110.1 // indirect
	k8s.io/utils v0.0.0-20231127182322-b307cd553661 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
)

replace (
	github.com/aws/aws-sdk-go => github.com/aws/aws-sdk-go v1.49.19
	github.com/gorilla/websocket => github.com/gorilla/websocket v1.5.1
	github.com/pkg/sftp => github.com/pkg/sftp v1.13.6
	golang.org/x/text => golang.org/x/text v0.14.0
	google.golang.org/grpc => google.golang.org/grpc v1.60.1
)
