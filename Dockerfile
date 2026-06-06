# SPDX-License-Identifier: Apache-2.0
# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
WORKDIR /src/editor/web
COPY editor/web/package.json editor/web/package-lock.json ./
RUN npm ci
COPY editor/web ./
RUN npm run build

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY editor ./editor
COPY --from=web /src/editor/web/dist ./editor/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/uvoo-hugo-editor ./editor

FROM alpine:3.21
RUN apk add --no-cache ca-certificates hugo && adduser -D -H -u 10001 uvoo-hugo
WORKDIR /app
COPY --from=build /out/uvoo-hugo-editor /app/uvoo-hugo-editor
RUN mkdir -p /site && chown -R uvoo-hugo:uvoo-hugo /app /site
USER uvoo-hugo
ENV UVOO_HUGO_EDITOR_ADDR=:1314 \
    UVOO_HUGO_EDITOR_SITE=/site \
    UVOO_HUGO_EDITOR_HUGO_ADDR=127.0.0.1:1313
EXPOSE 1314
VOLUME ["/site"]
ENTRYPOINT ["/app/uvoo-hugo-editor"]
