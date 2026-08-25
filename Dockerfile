# syntax=docker/dockerfile:1
# Copyright 2026 Jaziel Guerrero
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/workdock . \
    && GOBIN=/out go install github.com/jackc/tern/v2@latest

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN groupadd -g 65532 workdock && useradd -u 65532 -g workdock -d /app/config workdock
COPY --from=build /out/workdock /usr/local/bin/workdock
COPY --from=build /out/tern /usr/local/bin/tern
COPY migrations /app/migrations
# Compose mounts docker/workdock at /app/config, keeping credentials out of the image.
WORKDIR /app/config
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/workdock"]
