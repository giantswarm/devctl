FROM gsoci.azurecr.io/giantswarm/alpine:3.23.2

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
