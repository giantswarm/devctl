FROM quay.io/giantswarm/alpine:3.18.4

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
