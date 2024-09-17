FROM gsoci.azurecr.io/giantswarm/alpine:3.20.3

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
