BIN_DIR := bin
RPI_HOST ?= led@192.168.0.18
RPI_CONFIG ?= config.toml
RPI_SSH_PORT ?= 22
RPI_HEALTH_URL ?= http://127.0.0.1:8080/healthz
RPI_REMOTE_PATH ?= /usr/local/go/bin:/usr/local/bin:/usr/bin:/bin

.PHONY: test build build-server build-client run-server native build-rpi deploy-rpi check-deploy-scripts

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

deploy-rpi:
	@test -n "$(RPI_HOST)" || (echo "usage: make deploy-rpi RPI_HOST=pi@raspberrypi.local RPI_CONFIG=server.toml"; exit 2)
	RPI_SSH_PORT="$(RPI_SSH_PORT)" RPI_HEALTH_URL="$(RPI_HEALTH_URL)" RPI_REMOTE_PATH="$(RPI_REMOTE_PATH)" ./scripts/deploy-rpi.sh "$(RPI_HOST)" "$(RPI_CONFIG)"

check-deploy-scripts:
	sh -n scripts/deploy-rpi.sh deploy/install-rpi.sh
