.PHONY: test check build run

test:
	go test -race ./...
	cd third_party/openapi-go && go test ./quote -run TestGatewayTurnoverNotOverwritten
check:
	go vet ./...
	go test -race ./...
	cd third_party/openapi-go && go test ./quote -run TestGatewayTurnoverNotOverwritten
build:
	CGO_ENABLED=0 go build -trimpath -o bin/quote-gateway ./cmd/server
run:
	go run ./cmd/server
