FROM quay.io/giantswarm/alpine:3.17.2

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
