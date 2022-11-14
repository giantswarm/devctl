FROM quay.io/giantswarm/alpine:3.16.3

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
