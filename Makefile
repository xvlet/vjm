# Makefile for vjm (Vegeta-JMeter Engine)

APP_NAME = vjm
CMD_PATH = ./cmd/vjm/main.go
BASE_DIR = build
STAGING_DIR = build_staging
LDFLAGS = -w -s

.PHONY: all clean linux aix compile_linux compile_aix

all: clean linux aix

linux: compile_linux
	@echo "Build successful. Deploying to $(BASE_DIR)..."
	@mkdir -p $(BASE_DIR)
	@cp -r $(STAGING_DIR)/* $(BASE_DIR)/
	@rm -rf $(STAGING_DIR)
	@echo "Deployed successfully to $(BASE_DIR)."

aix: compile_aix
	@echo "Build successful. Deploying to $(BASE_DIR)..."
	@mkdir -p $(BASE_DIR)
	@cp -r $(STAGING_DIR)/* $(BASE_DIR)/
	@rm -rf $(STAGING_DIR)
	@echo "Deployed successfully to $(BASE_DIR)."

compile_linux:
	@echo "Starting build process in staging directory: $(STAGING_DIR)..."
	@rm -rf $(STAGING_DIR)
	@mkdir -p $(STAGING_DIR)
	@echo "Performing Go build for Linux (CGO_ENABLED=0)..."
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(STAGING_DIR)/$(APP_NAME)_linux $(CMD_PATH)

compile_aix:
	@echo "Starting build process in staging directory: $(STAGING_DIR)..."
	@rm -rf $(STAGING_DIR)
	@mkdir -p $(STAGING_DIR)
	@echo "Performing Go build for AIX (CGO_ENABLED=0, GOPPC64=power8)..."
	@GOOS=aix GOARCH=ppc64 GOPPC64=power8 CGO_ENABLED=0 go build -ldflags="-w" -o $(STAGING_DIR)/$(APP_NAME)_aix $(CMD_PATH)

clean:
	rm -rf $(BASE_DIR) $(STAGING_DIR)

