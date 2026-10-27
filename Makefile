.PHONY: test integration fmt clean

test:
	go test -race ./...

integration:
	scripts/integration.sh

fmt:
	gofmt -w .

clean:
	rm -rf .cache
