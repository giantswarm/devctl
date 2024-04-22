.PHONY: generate-go
generate-go: # Generate template files needed to run
	go generate ./...

$(SOURCES): generate-go

install: generate-go
