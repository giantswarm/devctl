FROM gsoci.azurecr.io/giantswarm/alpine:3.24.3

ARG TARGETARCH

# devctl serve's reservation tools clone, render and push with git.
RUN apk add --no-cache git

COPY ./devctl-linux-${TARGETARCH} /usr/bin/devctl

ENTRYPOINT ["devctl"]
