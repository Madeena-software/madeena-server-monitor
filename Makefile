# ============================================================
# Madeena Server Monitor – Makefile
# ============================================================

BINARY     := monitor
CMD_PATH   := ./cmd/monitor
BUILD_DIR  := ./bin

PROTO_DIR  := ./proto
PROTO_OUT  := ./proto/monitorpb
PROTO_FILE := $(PROTO_DIR)/monitor.proto

GO         := go
GOFLAGS    :=

.PHONY: all build run test clean protoc

all: build

## build: Compile the monitor binary into ./bin/monitor
build:
	@echo "==> Building $(BINARY) …"
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD_PATH)
	@echo "==> Done: $(BUILD_DIR)/$(BINARY)"

## run: Build then run the monitor (requires a valid .env file)
run: build
	@echo "==> Running $(BUILD_DIR)/$(BINARY) …"
	$(BUILD_DIR)/$(BINARY)

## test: Run all unit tests
test:
	@echo "==> Running tests …"
	$(GO) test -v ./...

## clean: Remove build artefacts
clean:
	@echo "==> Cleaning …"
	@rm -rf $(BUILD_DIR)

## protoc: Generate Go stubs from proto/monitor.proto
##         Requires: protoc, protoc-gen-go, protoc-gen-go-grpc
protoc:
	@echo "==> Generating protobuf stubs …"
	@mkdir -p $(PROTO_OUT)
	protoc \
		--go_out=$(PROTO_OUT) --go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_OUT) --go-grpc_opt=paths=source_relative \
		$(PROTO_FILE)
	@echo "==> Stubs written to $(PROTO_OUT)"
