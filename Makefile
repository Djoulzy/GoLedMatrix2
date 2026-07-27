.PHONY: test build run-server native build-rpi

test:
	go test -race ./...

build:
	go build ./cmd/...

run-server:
	go run ./cmd/ledmatrix-server -backend memory

# Run after: git submodule update --init --recursive && make native
build-rpi:
	CGO_ENABLED=1 \
	CGO_CFLAGS="-I$(CURDIR)/third_party/_rpi-rgb-led-matrix/include" \
	CGO_LDFLAGS="-L$(CURDIR)/third_party/_rpi-rgb-led-matrix/lib" \
	go build -tags rpi -o bin/ledmatrix-server ./cmd/ledmatrix-server

native:
	$(MAKE) -C third_party/_rpi-rgb-led-matrix/lib
