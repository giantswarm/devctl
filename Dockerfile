FROM quay.io/giantswarm/alpine:3.18.3

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
