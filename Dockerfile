FROM quay.io/giantswarm/alpine:3.17.3

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
