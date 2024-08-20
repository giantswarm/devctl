FROM gsoci.azurecr.io/giantswarm/alpine:3.20.2

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
