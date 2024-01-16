FROM gsoci.azurecr.io/giantswarm/alpine:3.19.0

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
