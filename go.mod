module github.com/giantswarm/devctl/v7

go 1.23.0

toolchain go1.24.0

require (
	github.com/Masterminds/semver/v3 v3.3.1
	github.com/aws/aws-sdk-go v1.55.6
	github.com/blang/semver v3.5.1+incompatible
	github.com/bmatcuk/doublestar/v4 v4.8.1
	github.com/buger/goterm v1.0.4
	github.com/fatih/color v1.18.0
	github.com/giantswarm/microerror v0.4.1
	github.com/giantswarm/micrologger v1.1.2
	github.com/giantswarm/release-operator/v4 v4.2.0
	github.com/google/go-github/v69 v69.2.0
	github.com/jedib0t/go-pretty/v6 v6.6.6
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826
	github.com/pelletier/go-toml/v2 v2.2.3
	github.com/rhysd/go-github-selfupdate v1.2.3
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/cobra v1.9.1
	github.com/spf13/pflag v1.0.6
	golang.org/x/net v0.36.0
	golang.org/x/oauth2 v0.27.0
	k8s.io/apimachinery v0.32.2
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/giantswarm/k8smetadata v0.24.0 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/go-github/v30 v30.1.0 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/inconshreveable/go-update v0.0.0-20160112193335-8152e7eb6ccf // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/tcnksm/go-gitconfig v0.1.2 // indirect
	github.com/ulikunitz/xz v0.5.9 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	golang.org/x/crypto v0.35.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/utils v0.0.0-20241104100929-3ea5e8cea738 // indirect
	sigs.k8s.io/json v0.0.0-20241010143419-9aa6b5e7a4b3 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.2 // indirect
)

replace (
	github.com/aws/aws-sdk-go => github.com/aws/aws-sdk-go v1.55.6
	github.com/gorilla/websocket => github.com/gorilla/websocket v1.5.3
	github.com/pkg/sftp => github.com/pkg/sftp v1.13.7
	golang.org/x/text => golang.org/x/text v0.22.0
)
