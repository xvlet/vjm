# Makefile for vjm (Vegeta-JMeter Engine)

APP_NAME = vjm
CMD_PATH = ./cmd/vjm/main.go
BASE_DIR = build
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
LDFLAGS = -w -s -X main.Version=$(VERSION)
export GOTOOLCHAIN=auto

# SBOM, Vulnerability
MKDOCDIR = mkdir -p
SBOM_DIR = docs

.PHONY: all clean check docs linux_amd64 linux_arm64 darwin_amd64 darwin_arm64 windows_amd64 windows_arm64 aix_ppc64

all: clean linux_amd64 linux_arm64 darwin_amd64 darwin_arm64 windows_amd64 windows_arm64 aix_ppc64

linux_amd64:
	@echo "Building for linux/amd64..."
	@rm -rf build_staging_$@
	@mkdir -p build_staging_$@ $(BASE_DIR)
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o build_staging_$@/$(APP_NAME) $(CMD_PATH)
	@tar -czf $(BASE_DIR)/$(APP_NAME)_linux_amd64.tar.gz -C build_staging_$@ $(APP_NAME)
	@rm -rf build_staging_$@

linux_arm64:
	@echo "Building for linux/arm64..."
	@rm -rf build_staging_$@
	@mkdir -p build_staging_$@ $(BASE_DIR)
	@GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o build_staging_$@/$(APP_NAME) $(CMD_PATH)
	@tar -czf $(BASE_DIR)/$(APP_NAME)_linux_arm64.tar.gz -C build_staging_$@ $(APP_NAME)
	@rm -rf build_staging_$@

darwin_amd64:
	@echo "Building for darwin/amd64 (Mac Intel)..."
	@rm -rf build_staging_$@
	@mkdir -p build_staging_$@ $(BASE_DIR)
	@GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o build_staging_$@/$(APP_NAME) $(CMD_PATH)
	@tar -czf $(BASE_DIR)/$(APP_NAME)_darwin_amd64.tar.gz -C build_staging_$@ $(APP_NAME)
	@rm -rf build_staging_$@

darwin_arm64:
	@echo "Building for darwin/arm64 (Mac Apple Silicon)..."
	@rm -rf build_staging_$@
	@mkdir -p build_staging_$@ $(BASE_DIR)
	@GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o build_staging_$@/$(APP_NAME) $(CMD_PATH)
	@tar -czf $(BASE_DIR)/$(APP_NAME)_darwin_arm64.tar.gz -C build_staging_$@ $(APP_NAME)
	@rm -rf build_staging_$@

windows_amd64:
	@echo "Building for windows/amd64..."
	@rm -rf build_staging_$@
	@mkdir -p build_staging_$@ $(BASE_DIR)
	@GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o build_staging_$@/$(APP_NAME).exe $(CMD_PATH)
	@cd build_staging_$@ && zip -q ../$(BASE_DIR)/$(APP_NAME)_windows_amd64.zip $(APP_NAME).exe
	@rm -rf build_staging_$@

windows_arm64:
	@echo "Building for windows/arm64..."
	@rm -rf build_staging_$@
	@mkdir -p build_staging_$@ $(BASE_DIR)
	@GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o build_staging_$@/$(APP_NAME).exe $(CMD_PATH)
	@cd build_staging_$@ && zip -q ../$(BASE_DIR)/$(APP_NAME)_windows_arm64.zip $(APP_NAME).exe
	@rm -rf build_staging_$@

aix_ppc64:
	@echo "Building for aix/ppc64..."
	@rm -rf build_staging_$@
	@mkdir -p build_staging_$@ $(BASE_DIR)
	@GOOS=aix GOARCH=ppc64 GOPPC64=power8 CGO_ENABLED=0 go build -ldflags="-w" -o build_staging_$@/$(APP_NAME) $(CMD_PATH)
	@tar -czf $(BASE_DIR)/$(APP_NAME)_aix_ppc64.tar.gz -C build_staging_$@ $(APP_NAME)
	@rm -rf build_staging_$@

clean:
	rm -rf $(BASE_DIR) build_staging_*

check:
	golangci-lint run
	-gosec -exclude=G115,G117,G204,G301,G302,G304,G306,G401,G404,G501,G505,G602,G704 ./...
	-govulncheck ./...
	-gitleaks detect --source . || echo "gitleaks issues detected"

docs:
	$(MKDOCDIR) $(SBOM_DIR)
	@echo "Generating SBOM (Filesystem)..."
	trivy fs --format cyclonedx \
		-o $(SBOM_DIR)/$(APP_NAME)-fs.cdx.json .
	trivy fs --format spdx-json \
		-o $(SBOM_DIR)/$(APP_NAME)-fs.spdx.json .

	@echo "Generating Vulnerability Report..."
	trivy fs \
		--scanners vuln \
		--pkg-types library \
		--ignore-unfixed \
		--format json \
		-o $(SBOM_DIR)/trivy-vuln.json \
		.
