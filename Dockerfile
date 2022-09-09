FROM quay.io/giantswarm/alpine:3.16.2

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
