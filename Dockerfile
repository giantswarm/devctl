FROM gsoci.azurecr.io/giantswarm/alpine:3.24.3

ARG TARGETARCH

COPY ./devctl-linux-${TARGETARCH} /usr/bin/devctl

ENTRYPOINT ["devctl"]
