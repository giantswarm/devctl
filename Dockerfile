FROM quay.io/giantswarm/alpine:3.16.1

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
