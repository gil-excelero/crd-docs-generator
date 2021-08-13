
build: cmd/main.go cmd/parser.go cmd/renderer.go
	cd cmd && go build -o ../crd-docs-generator

docker-build:
	docker build . --tag=crd-doc-genenrator
