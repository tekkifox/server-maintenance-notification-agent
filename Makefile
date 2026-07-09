GO ?= go
SWAG ?= $(shell $(GO) env GOPATH)/bin/swag
STACK_ENV ?= stack.env

.PHONY: help run swag test tidy

help:
	@printf '%s\n' "Targets:" \
		"  run   Run the notification agent locally with $(STACK_ENV)" \
		"  swag  Regenerate Swagger docs into ./docs" \
		"  test  Run the full Go test suite" \
		"  tidy  Run go mod tidy"

run:
	set -a; if [ -f $(STACK_ENV) ]; then . ./$(STACK_ENV); fi; set +a; $(GO) run ./cmd/notification-agent

swag:
	$(SWAG) init --generalInfo cmd/notification-agent/main.go --output docs --parseInternal --parseDependency --dir .

test:
	$(GO) test ./...

tidy:
	$(GO) mod tidy
