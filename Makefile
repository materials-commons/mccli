.PHONY: bin test all fmt deploy docs run

all: fmt cli

fmt:
	-go fmt ./...

cli:
	(cd ./cmd/mc2; go build)
