module github.com/giantswarm/devctl

require (
	github.com/bmatcuk/doublestar v1.1.5
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/giantswarm/apiextensions v0.0.0-20200220082851-d6884ee11480 // indirect
	github.com/giantswarm/backoff v0.0.0-20200209120535-b7cb1852522d // indirect
	github.com/giantswarm/k8sclient v0.0.0-20200120104955-1542917096d6
	github.com/giantswarm/microerror v0.0.0-20190605150300-f446cc816a48
	github.com/giantswarm/micrologger v0.0.0-20181128163930-39ed6a99d31b
	github.com/go-logfmt/logfmt v0.4.0 // indirect
	github.com/google/go-cmp v0.3.0
	github.com/google/go-github v17.0.0+incompatible
	github.com/google/go-querystring v1.0.0 // indirect
	github.com/juju/errgo v0.0.0-20140925100237-08cceb5d0b53 // indirect
	github.com/pelletier/go-toml v1.2.0
	github.com/spf13/cobra v0.0.5
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45
	k8s.io/apimachinery v0.17.2
	sigs.k8s.io/controller-runtime v0.5.0 // indirect
)

replace (
	gopkg.in/fsnotify.v1 v1.4.7 => gopkg.in/fsnotify/fsnotify.v1 v1.4.7
	k8s.io/api => k8s.io/api v0.0.0-20200131193051-d9adff57e763
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20200131201446-6910daba737d
	k8s.io/apimachinery => k8s.io/apimachinery v0.17.3-beta.0.0.20200131192631-731dcecc2054
	k8s.io/apiserver => k8s.io/apiserver v0.0.0-20200131195721-b64b0ef70370
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.0.0-20200131202043-1dc23f43cc94
	k8s.io/client-go => k8s.io/client-go v0.17.0
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.0.0-20200131203830-fe5589c708de
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.0.0-20200131203557-3c6746d7c617
	k8s.io/code-generator => k8s.io/code-generator v0.17.3-beta.0.0.20200131192142-4ae19cfe9b46
	k8s.io/component-base => k8s.io/component-base v0.0.0-20200131194811-85b325a9731b
	k8s.io/cri-api => k8s.io/cri-api v0.17.3-beta.0.0.20200131204836-cb8a25f43f0e
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.0.0-20200131204100-4311b557c8ce
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.0.0-20200131200134-d62c64b672cc
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.0.0-20200131203333-c935c9222556
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.0.0-20200131202556-6b094e7591d1
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.0.0-20200131203102-8e9ee8fa0785
	k8s.io/kubectl => k8s.io/kubectl v0.0.0-20200131205129-9ef1401eb3ec
	k8s.io/kubelet => k8s.io/kubelet v0.0.0-20200131202828-eb1b5c1ce7fb
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.0.0-20200131204342-ef4bac7ed518
	k8s.io/metrics => k8s.io/metrics v0.0.0-20200131201757-ffbb7a48f604
	k8s.io/node-api => k8s.io/node-api v0.0.0-20200131204614-47835c5f2652
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.0.0-20200131200511-51b2302b2589
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.0.0-20200131202323-14126e90c844
	k8s.io/sample-controller => k8s.io/sample-controller v0.0.0-20200131200932-3fd12213be16

)

go 1.13
