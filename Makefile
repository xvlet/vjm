# Makefile for vjm (Vegeta-JMeter Engine)

APP_NAME = vjm
CMD_PATH = ./cmd/vjm/main.go
BASE_DIR = build
STAGING_DIR_LINUX = build_staging_linux
STAGING_DIR_AIX = build_staging_aix
LDFLAGS = -w -s

.PHONY: all clean linux aix compile_linux compile_aix

all: clean linux aix

linux: compile_linux
	@echo "Build successful. Deploying to $(BASE_DIR)..."
	@mkdir -p $(BASE_DIR)
	@tar -czf $(BASE_DIR)/$(APP_NAME)_linux_amd64.tar.gz -C $(STAGING_DIR_LINUX) $(APP_NAME)
	@rm -rf $(STAGING_DIR_LINUX)
	@echo "Deployed successfully to $(BASE_DIR)."

aix: compile_aix
	@echo "Build successful. Deploying to $(BASE_DIR)..."
	@mkdir -p $(BASE_DIR)
	@tar -czf $(BASE_DIR)/$(APP_NAME)_aix_ppc64.tar.gz -C $(STAGING_DIR_AIX) $(APP_NAME)
	@rm -rf $(STAGING_DIR_AIX)
	@echo "Deployed successfully to $(BASE_DIR)."

compile_linux:
	@echo "Starting build process for Linux..."
	@rm -rf $(STAGING_DIR_LINUX)
	@mkdir -p $(STAGING_DIR_LINUX)
	@echo "Performing Go build for Linux (CGO_ENABLED=0)..."
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(STAGING_DIR_LINUX)/$(APP_NAME) $(CMD_PATH) >> build_linux.log 2>&1

compile_aix:
	@echo "Starting build process for AIX..."
	@rm -rf $(STAGING_DIR_AIX)
	@mkdir -p $(STAGING_DIR_AIX)
	@echo "Performing Go build for AIX (CGO_ENABLED=0, GOPPC64=power8)..."
	@GOOS=aix GOARCH=ppc64 GOPPC64=power8 CGO_ENABLED=0 go build -ldflags="-w" -o $(STAGING_DIR_AIX)/$(APP_NAME) $(CMD_PATH) >> build_aix.log 2>&1

clean:
	rm -rf $(BASE_DIR) $(STAGING_DIR_LINUX) $(STAGING_DIR_AIX)

