generate-go:
	go generate ./...

$(SOURCES): generate-go

install: generate-go
