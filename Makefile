.PHONY: run test vet opa-test

run:
	go run ./cmd/senseguard

test:
	go test ./cmd/senseguard/...

vet:
	go vet ./cmd/senseguard/...

opa-test:
	opa test policy/ -v
