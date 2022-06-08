module github.com/giantswarm/devctl

go 1.16

require (
	github.com/BurntSushi/toml v1.1.0 // indirect
	github.com/CloudyKit/jet/v3 v3.0.1 // indirect
	github.com/Masterminds/semver/v3 v3.1.1
	github.com/Shopify/goreferrer v0.0.0-20210630161223-536fa16abd6f // indirect
	github.com/armon/go-metrics v0.4.0 // indirect
	github.com/aws/aws-sdk-go v1.44.29 // indirect
	github.com/blang/semver v3.5.1+incompatible
	github.com/bmatcuk/doublestar v1.3.4
	github.com/cenkalti/backoff/v3 v3.2.2 // indirect
	github.com/fatih/color v1.13.0
	github.com/giantswarm/k8sclient/v7 v7.0.1
	github.com/giantswarm/kubeconfig/v4 v4.1.0
	github.com/giantswarm/microerror v0.4.0
	github.com/giantswarm/micrologger v0.6.0
	github.com/giantswarm/release-operator/v3 v3.2.0
	github.com/gin-gonic/gin v1.8.1 // indirect
	github.com/go-ldap/ldap/v3 v3.4.3 // indirect
	github.com/go-playground/validator/v10 v10.11.0 // indirect
	github.com/google/go-github v17.0.0+incompatible
	github.com/hashicorp/consul/api v1.13.0 // indirect
	github.com/hashicorp/consul/sdk v0.9.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.2.1 // indirect
	github.com/hashicorp/go-plugin v1.4.4 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.1 // indirect
	github.com/hashicorp/go-secure-stdlib/mlock v0.1.2 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.1.6 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/hashicorp/go-version v1.5.0 // indirect
	github.com/hashicorp/serf v0.9.8 // indirect
	github.com/hashicorp/vault/api v1.6.0 // indirect
	github.com/hashicorp/vault/sdk v0.5.0 // indirect
	github.com/hashicorp/yamux v0.0.0-20211028200310-0bc27b27de87 // indirect
	github.com/iris-contrib/jade v1.1.4 // indirect
	github.com/iris-contrib/schema v0.0.6 // indirect
	github.com/kataras/golog v0.1.7 // indirect
	github.com/kataras/iris/v12 v12.1.8 // indirect
	github.com/klauspost/compress v1.15.6 // indirect
	github.com/labstack/echo/v4 v4.7.2 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/microcosm-cc/bluemonday v1.0.18 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/go-testing-interface v1.14.1 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/nats-io/jwt v1.2.2 // indirect
	github.com/nats-io/nats-server/v2 v2.8.4 // indirect
	github.com/nats-io/nkeys v0.3.0 // indirect
	github.com/oklog/run v1.1.0 // indirect
	github.com/pelletier/go-toml v1.9.4
	github.com/pierrec/lz4 v2.6.1+incompatible // indirect
	github.com/pkg/sftp v1.13.4 // indirect
	github.com/prometheus/client_golang v1.12.2 // indirect
	github.com/rhysd/go-github-selfupdate v1.2.3
	github.com/ryanuber/columnize v2.1.2+incompatible // indirect
	github.com/spf13/cobra v1.4.0
	github.com/spf13/pflag v1.0.5
	github.com/urfave/negroni v1.0.0 // indirect
	github.com/valyala/fasthttp v1.37.0 // indirect
	golang.org/x/crypto v0.0.0-20220525230936-793ad666bf5e // indirect
	golang.org/x/net v0.0.0-20220607020251-c690dde0001d
	golang.org/x/oauth2 v0.0.0-20211104180415-d3ed0bb246c8
	golang.org/x/time v0.0.0-20220411224347-583f2d630306 // indirect
	google.golang.org/genproto v0.0.0-20220607223854-30acc4cbd2aa // indirect
	gopkg.in/ini.v1 v1.66.6 // indirect
	gopkg.in/square/go-jose.v2 v2.6.0 // indirect
	k8s.io/apimachinery v0.20.12
	sigs.k8s.io/yaml v1.3.0
)

replace (
	github.com/coreos/etcd v3.3.13+incompatible => github.com/coreos/etcd v3.3.25+incompatible
	github.com/dgrijalva/jwt-go => github.com/dgrijalva/jwt-go/v4 v4.0.0-preview1

	// Mitigate a security issue in github.com/gorilla/websocket v1.4.0 and earlier
	github.com/gorilla/websocket => github.com/gorilla/websocket v1.4.2
)
