build:
    go build -o lz .

publish: build
    cp lz ~/.local/bin/

vet:
    go vet ./...
