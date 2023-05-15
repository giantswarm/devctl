FROM quay.io/giantswarm/alpine:3.18.0

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
