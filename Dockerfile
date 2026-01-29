FROM gsoci.azurecr.io/giantswarm/alpine:3.23.3

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
