FROM quay.io/giantswarm/alpine:3.17.1

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
