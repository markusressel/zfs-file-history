GO_FLAGS   ?=
NAME       := zfs-file-history
OUTPUT_BIN ?= bin/${NAME}
PACKAGE    := github.com/markusressel/$(NAME)
GIT_REV    ?= $(shell git rev-parse --short HEAD)
SOURCE_DATE_EPOCH ?= $(shell date +%s)
DATE       ?= $(shell date -u -d @${SOURCE_DATE_EPOCH} +"%Y-%m-%dT%H:%M:%SZ")
VERSION    ?= 0.0.1

test:   ## Run all tests
	@go clean --testcache && go test -v ./...

build:  ## Builds the CLI
	@go build ${GO_FLAGS} \
	-ldflags "-w -s -X ${PACKAGE}/cmd.version=${VERSION} -X ${PACKAGE}/cmd.commit=${GIT_REV} -X ${PACKAGE}/cmd.date=${DATE}" \
	-a -tags netgo -o ${OUTPUT_BIN} main.go

run:
	go build -o ${OUTPUT_BIN} main.go
	./${OUTPUT_BIN}

deploy: build
	cp ./${OUTPUT_BIN} ~/.custom/bin/

clean:
	go clean
	rm ${OUTPUT_BIN}