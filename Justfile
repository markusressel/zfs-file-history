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
VERSION := "2.9.0"
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

# Run all tests with the race detector, without the test cache
test-race:
    go test -race -count=1 ./...

# Fail if a file is not formatted with gofmt
fmt-check:
    @files="$(gofmt -l .)"; if [ -n "$files" ]; then echo "Not formatted with gofmt:"; echo "$files"; exit 1; fi

# Run go vet
vet:
    go vet ./...

# What CI checks (except codespell), to run before a commit
check: fmt-check vet test-race demo-test

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
# The storyboards are in scripts/demo/storyboards.py, run by driver.py (its Python environment is managed by uv):
# "overview" walks through the whole application (the GIF of the README), the others show one feature each (see
# demo-list). Recorded in the terminal size of the storyboard. A storyboard may wait until a scrub (or another
# operation on a pool) runs, or until a pool reports a problem (alerts), which can be started after this; FLAGS go to
# the driver (--without-activity: do not wait, unless the storyboard is about it). Fails if a screen it waits for did
# not show up.
# Record a storyboard of the demo with asciinema to bin/demo-<name>.cast
demo-record NAME="overview" *FLAGS: (build "") demo-test
    #!/usr/bin/env bash
    set -euo pipefail
    uv sync --quiet --frozen --project scripts/demo
    driver="uv run --quiet --frozen --project scripts/demo python scripts/demo/driver.py"
    read -r size activity alert < <($driver --describe {{NAME}})
    rm -f bin/demo.error
    if [ "$activity" = required ] || [[ "$activity" = optional && " {{FLAGS}} " != *" --without-activity "* ]]; then
        echo "Recording {{NAME}}: start a scrub now (e.g. zpool scrub <pool>), the demo waits up to 5 minutes for it"
    elif [ "$alert" = alert ]; then
        echo "Recording {{NAME}}: make a pool report a problem now (e.g. take a device of a mirror offline for a" \
            "moment), the demo waits up to 5 minutes for the alert"
    else
        echo "Recording {{NAME}}"
    fi
    asciinema rec --headless --overwrite --window-size "$size" --idle-time-limit 2 --quiet \
        --command "$driver --storyboard {{NAME}} {{FLAGS}} {{OUTPUT_BIN}} . bin/demo.error" bin/demo-{{NAME}}.cast
    if [ -s bin/demo.error ]; then cat bin/demo.error; exit 1; fi

# List the storyboards of the demo
demo-list:
    @uv run --quiet --frozen --project scripts/demo python scripts/demo/driver.py --list

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

# The overview is rendered as screenshots/demo.gif (the README), the others as screenshots/demo-<name>.gif. agg
# renders at twice the font size, ffmpeg (render.py) zooms in (steps.Zoom), draws the keys typed and scales it to
# half, so zoomed text stays sharp.
# Render bin/demo-<name>.cast as a GIF with agg
demo-gif NAME="overview": agg
    #!/usr/bin/env bash
    set -euo pipefail
    python="uv run --quiet --frozen --project scripts/demo python"
    gif=screenshots/demo-{{NAME}}.gif
    if [ {{NAME}} = overview ]; then gif=screenshots/demo.gif; fi
    prepared=bin/demo-{{NAME}}-gif.cast
    $python scripts/demo/prepare_cast.py bin/demo-{{NAME}}.cast $prepared
    {{AGG}} --theme {{AGG_THEME}} --font-size 28 --idle-time-limit 2 --fps-cap 15 $prepared bin/demo-{{NAME}}-large.gif
    $python scripts/demo/render.py $prepared.render.json 28 bin/demo-{{NAME}}-large.gif "$gif"
    echo "$gif"

# E.g. "just demo files", "just demo overview --without-activity"; see demo-record and demo-gif.
# Record a storyboard of the demo and render it as a GIF
demo NAME="overview" *FLAGS: (demo-record NAME FLAGS) (demo-gif NAME)
