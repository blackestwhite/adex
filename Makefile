.PHONY: test build qdrant graph doctor index graph-doctor graph-index

test:
	go test ./...

build:
	go build ./cmd/adex

qdrant:
	docker compose up -d qdrant

graph:
	docker compose --profile graph up -d

doctor:
	go run ./cmd/adex doctor

index:
	go run ./cmd/adex index -root .

graph-doctor:
	go run ./cmd/adex graph-doctor

graph-index:
	go run ./cmd/adex graph-index -root .
