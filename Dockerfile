FROM alpine:3.8

RUN apk add --no-cache ca-certificates

ADD ./devctl /usr/local/bin/devctl
ENTRYPOINT ["/usr/local/bin/devctl"]
