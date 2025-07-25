FROM gsoci.azurecr.io/giantswarm/alpine:3.22.1

COPY ./devctl /usr/bin/devctl

ENTRYPOINT ["devctl"]
