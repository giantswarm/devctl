FROM alpine:3.8

RUN apk update && apk --no-cache add ca-certificates && \
  update-ca-certificates

ADD ./devctl /usr/local/bin/devctl
ENTRYPOINT ["/usr/local/bin/devctl"]
