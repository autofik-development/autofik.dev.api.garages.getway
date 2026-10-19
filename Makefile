# Load .env if it exists
ifneq (,$(wildcard .env))
include .env
export
endif

MODULE := github.com/autofik-development/autofik.dev.api.garages.getway
LOCAL_IMAGE := $(IMAGE_NAME):$(IMAGE_TAG)
REMOTE_IMAGE := $(DOCKER_USERNAME_TAG)/$(IMAGE_NAME):$(IMAGE_TAG)

# protoc plugins (protoc-gen-go, protoc-gen-go-grpc, protoc-gen-grpc-gateway)
# are installed by `go install` into GOPATH/bin, which is often not on PATH.
export PATH := $(PATH):$(shell go env GOPATH)/bin

.PHONY: proto login build tag push clean delete-hub all release test vet fmt check run

# Every .proto under internal/grpc/proto, so a new API version (e.g.
# garages/v2/) is generated without touching this file.
PROTO_FILES := $(shell find internal/grpc/proto -name '*.proto')

# Regenerate internal/grpc/pb/garages/<version> from internal/grpc/proto
proto:
	@echo "Generating protobuf code..."
	protoc -I internal/grpc/proto -I pkg/googleapis \
		--go_out=. --go_opt=module=$(MODULE) \
		--go-grpc_out=. --go-grpc_opt=module=$(MODULE) \
		--grpc-gateway_out=. --grpc-gateway_opt=module=$(MODULE) \
		$(PROTO_FILES)

# Run locally
run:
	go run ./cmd

# Login Docker Hub
login:
	@echo "Make Logging in to Docker Hub..."
	@echo "$(DOCKER_TOKEN)" | docker login -u "$(DOCKER_USERNAME_LOGIN)" --password-stdin


# Format, vet and test the Go sources
fmt:
	@echo "Checking formatting..."
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi

vet:
	@echo "Vetting..."
	go vet ./...

test:
	@echo "Running tests..."
	go test -race -count=1 ./...

check: fmt vet test

# Build Image - gated on the test suite so a broken tree never becomes an image
build: check
	@echo "Building image..."
	docker build --build-arg VERSION=$(IMAGE_TAG) -t $(LOCAL_IMAGE) .

# Tag Image
tag:
	@echo "Tagging image..."
	docker tag $(LOCAL_IMAGE) $(REMOTE_IMAGE)

# Push Image
push: build tag
	@echo "Pushing image..."
	docker push $(REMOTE_IMAGE)


# Remove Local Images
clean:
	@echo "Removing local images..."

	-docker rmi $(LOCAL_IMAGE)
	-docker rmi $(REMOTE_IMAGE)

	@echo "Cleaning build cache..."

	-docker builder prune -f
	-docker image prune -f

# Complete Pipeline
all:
	@set -e; \
	echo "===================================="; \
	echo "BUILD"; \
	echo "===================================="; \
	$(MAKE) build; \
	\
	echo "===================================="; \
	echo "TAG"; \
	echo "===================================="; \
	$(MAKE) tag; \
	\
	echo "===================================="; \
	echo "PUSH"; \
	echo "===================================="; \
	$(MAKE) push; \
	\
	echo "===================================="; \
	echo "CLEAN"; \
	echo "===================================="; \
	$(MAKE) clean; \
	\
	echo "Pipeline completed successfully"

# Build + Push, then deploy the full stack from the repo root
release:
	@set -e; \
	$(MAKE) push; \
	$(MAKE) -C .. deploy; \
	$(MAKE) clean

# Delete Tag From Docker Hub
delete-hub:
	@if [ -z "$(DOCKER_TOKEN)" ]; then \
		echo "DOCKER_TOKEN is required"; \
		exit 1; \
	fi

	curl -X DELETE \
		-H "Authorization: Bearer $(DOCKER_TOKEN)" \
		https://hub.docker.com/v2/repositories/$(DOCKER_USERNAME_TAG)/$(IMAGE_NAME)/tags/$(IMAGE_TAG)/

	@echo ""
	@echo "Deleted tag $(IMAGE_TAG)"
