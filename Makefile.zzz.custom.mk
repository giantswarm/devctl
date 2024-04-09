generate-go:
	go generate ./...

$(SOURCES): generate-go
