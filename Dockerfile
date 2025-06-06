FROM gsoci.azurecr.io/giantswarm/alpine:3.22.0

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
