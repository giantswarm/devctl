FROM gsoci.azurecr.io/giantswarm/alpine:3.23.4

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
