FROM gsoci.azurecr.io/giantswarm/alpine:3.23.4

ARG TARGETARCH

COPY ./devctl-linux-${TARGETARCH} /usr/bin/devctl

ENTRYPOINT ["devctl"]
