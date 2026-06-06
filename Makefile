# SPDX-License-Identifier: Apache-2.0

.PHONY: build ci docker-build license-check release test web web-install

APP_NAME ?= uvoo-hugo-editor

web:
	cd editor/web && npm ci && npm run build

web-install:
	cd editor/web && npm ci

build: web
	mkdir -p bin
	go build -trimpath -o ./bin/$(APP_NAME) ./editor

test:
	go test ./...

license-check:
	bash scripts/license-check.sh

ci: web test license-check

release:
	bash scripts/release.sh

docker-build:
	docker build -t $(APP_NAME):local .
