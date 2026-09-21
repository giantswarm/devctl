FROM gsoci.azurecr.io/giantswarm/alpine:3.24.2

ARG TARGETARCH

COPY ./devctl-linux-${TARGETARCH} /usr/bin/devctl

ENTRYPOINT ["devctl"]
