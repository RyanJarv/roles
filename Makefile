.PHONY: build

build:
	mkdir -p build/darwin-arm && go build -o build/darwin-arm/roles main.go
	mkdir -p build/linux-arm && GOOS=linux GOARCH=arm64 go build -o build/linux-arm/roles main.go
