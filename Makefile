.PHONY: run test vet opa-test

run:
	go run ./cmd/senseguard

test:
	go test ./...

vet:
	go vet ./...

opa-test:
	opa test policy/ -v
