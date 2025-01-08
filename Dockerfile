FROM gsoci.azurecr.io/giantswarm/alpine:3.21.1

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
