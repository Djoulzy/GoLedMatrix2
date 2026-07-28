BIN_DIR := bin

.PHONY: test build build-server build-client run-server native build-rpi

test:
	go test -race ./...

build: build-server build-client

build-server: | $(BIN_DIR)
	go build -o $(BIN_DIR)/ledmatrix-server ./cmd/ledmatrix-server

build-client: | $(BIN_DIR)
	go build -o $(BIN_DIR)/ledmatrix-client ./cmd/ledmatrix-client

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

run-server:
	go run ./cmd/ledmatrix-server -backend simulation -config config.toml

# Run after: git submodule update --init --recursive && make native
build-rpi: | $(BIN_DIR)
	CGO_ENABLED=1 \
	CGO_CFLAGS="-I$(CURDIR)/third_party/_rpi-rgb-led-matrix/include" \
	CGO_LDFLAGS="-L$(CURDIR)/third_party/_rpi-rgb-led-matrix/lib" \
	go build -tags rpi -o $(BIN_DIR)/ledmatrix-server ./cmd/ledmatrix-server

native:
	$(MAKE) -C third_party/_rpi-rgb-led-matrix/lib
