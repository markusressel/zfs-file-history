# Explicitly define variables so they accept command-line/matrix overrides
GOOS := "linux"
GOARCH := "amd64"
CC := "gcc"
CGO_ENABLED := "1"

GO_FLAGS := ""
NAME := "zfs-file-history"
OUTPUT_BIN := "bin/" + NAME
PACKAGE := "github.com/markusressel/" + NAME
GIT_REV := `git rev-parse --short HEAD`
DATE := `date -u +"%Y-%m-%dT%H:%M:%SZ"`
VERSION := "2.3.0"
# agg renders the demo as a GIF, see demo-gif: a release binary (https://github.com/asciinema/agg), downloaded into
# bin/ and checked against its checksum
AGG_VERSION := "1.9.0"
AGG_SHA256 := "f111e315cd71056b116302342553dd765b7297579ed511f111d0cedb442aeda6"
AGG := "bin/agg-" + AGG_VERSION
# the colors of the GIF: background, foreground and the 16 palette colors of kitty (agg defaults to dracula, which has
# a blue-gray background)
AGG_THEME := "000000,dddddd,000000,cc0403,19cb00,cecb00,0d73cc,cb1ed1,0dcdcd,dddddd,767676,f2201f,23fd00,fffd00,1a8fff,fd28ff,14ffff,ffffff"

# Run all tests
test:
    go clean --testcache
    go test -v ./...

# Run all tests with coverage and show summary
coverage:
    go test -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out

# Run all tests with coverage and open HTML report
coverage-html:
    go test -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out

# Builds the CLI with explicit architecture/compiler environment variables; REBUILD="" uses the build cache (-a
# rebuilds all packages)
build REBUILD="-a":
    @mkdir -p $(dirname {{OUTPUT_BIN}})
    CGO_ENABLED={{CGO_ENABLED}} GOOS={{GOOS}} GOARCH={{GOARCH}} CC={{CC}} \
    go build {{GO_FLAGS}} \
        -ldflags "-w -s \
        -X {{NAME}}/cmd/global.Version={{VERSION}} \
        -X {{PACKAGE}}/cmd/global.Version={{VERSION}} \
        -X {{NAME}}/cmd/global.Commit={{GIT_REV}} \
        -X {{PACKAGE}}/cmd/global.Commit={{GIT_REV}} \
        -X {{NAME}}/cmd/global.Date={{DATE}} \
        -X {{PACKAGE}}/cmd/global.Date={{DATE}}" \
        {{REBUILD}} -tags netgo -o {{OUTPUT_BIN}} main.go

# Build and run the CLI
run: build
    ./{{OUTPUT_BIN}}

# Deploy to custom bin directory
deploy-custom: clean build
    mkdir -p ~/.custom/bin/
    install -m 755 ./{{OUTPUT_BIN}} ~/.custom/bin/.{{NAME}}.new
    mv -f ~/.custom/bin/.{{NAME}}.new ~/.custom/bin/{{NAME}}

# Deploy to /usr/local/bin. Copied next to the target and renamed over it: overwriting a running binary fails with
# "Text file busy", a rename lets running instances keep the old one.
deploy: clean build
    sudo install -m 755 ./{{OUTPUT_BIN}} /usr/local/bin/.{{NAME}}.new
    sudo mv -f /usr/local/bin/.{{NAME}}.new /usr/local/bin/{{NAME}}

# Clean build artifacts
clean:
    go clean
    rm -f {{OUTPUT_BIN}}
# Record the demo (scripts/demo/driver.py, its Python environment is managed by uv) with asciinema to bin/demo.cast.
# It waits until a scrub (or another operation on a pool) runs, which can be started after this; FLAGS go to the
# driver (--without-activity: do not wait). Fails if a screen the demo waits for did not show up.
demo-record *FLAGS: (build "") demo-test
    uv sync --quiet --frozen --project scripts/demo
    rm -f bin/demo.error
    @echo "Recording: start a scrub now (e.g. zpool scrub <pool>), the demo waits up to 5 minutes for it"
    asciinema rec --headless --overwrite --window-size 179x42 --idle-time-limit 2 --quiet \
        --command "uv run --quiet --frozen --project scripts/demo python scripts/demo/driver.py {{FLAGS}} {{OUTPUT_BIN}} . bin/demo.error" bin/demo.cast
    @if [ -s bin/demo.error ]; then cat bin/demo.error; exit 1; fi

# Test and lint the demo driver (scripts/demo), without zfs-file-history
demo-test:
    cd scripts/demo && uv run --quiet --frozen python -m unittest
    cd scripts/demo && uv run --quiet --frozen ruff check .
    cd scripts/demo && uv run --quiet --frozen ruff format --check .

# Download agg into bin/, unless it is there already
agg:
    #!/usr/bin/env bash
    set -euo pipefail
    [ -x {{AGG}} ] && exit 0
    mkdir -p bin
    curl --fail --location --silent --show-error --output {{AGG}}.download \
        https://github.com/asciinema/agg/releases/download/v{{AGG_VERSION}}/agg-x86_64-unknown-linux-gnu
    echo "{{AGG_SHA256}}  {{AGG}}.download" | sha256sum --check --quiet
    chmod +x {{AGG}}.download
    mv {{AGG}}.download {{AGG}}

# Render bin/demo.cast as screenshots/demo.gif with agg
demo-gif: agg
    uv run --quiet --frozen --project scripts/demo python scripts/demo/prepare_cast.py bin/demo.cast bin/demo-gif.cast
    {{AGG}} --theme {{AGG_THEME}} --font-size 14 --idle-time-limit 2 --fps-cap 15 bin/demo-gif.cast screenshots/demo.gif

# Record the demo and render it as screenshots/demo.gif
demo *FLAGS: (demo-record FLAGS) demo-gif
