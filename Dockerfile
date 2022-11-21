FROM quay.io/giantswarm/alpine:3.17

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
