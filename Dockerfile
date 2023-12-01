FROM quay.io/giantswarm/alpine:3.18.5

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
