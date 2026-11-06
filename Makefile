.PHONY: build test race vet fmt integration demo benchmark check
build:
	go build -trimpath -ldflags "-X github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/app.Version=0.1.0 -X github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/app.Commit=$$(git rev-parse --short HEAD) -X github.com/ranajahanzaib/ledger-to-chain-reconciler/internal/app.BuildDate=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o bin/ledgerchain ./cmd/ledgerchain
test:
	go test ./...
race:
	go test -race ./...
vet:
	go vet ./...
fmt:
	gofmt -w cmd internal tests
integration:
	go test -tags=integration -timeout=5m -v ./tests/integration
demo: build
	python3 script/demo.py
benchmark:
	go test -run '^$$' -bench=. -benchmem ./internal/reconcile
check: vet test
	test -z "$$(gofmt -l cmd internal tests)"
