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
VERSION := "2.1.0"
# agg renders the demo as a GIF, see demo-gif
AGG := "agg"

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

# Builds the CLI with explicit architecture/compiler environment variables
build:
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
        -a -tags netgo -o {{OUTPUT_BIN}} main.go

# Build and run the CLI
run: build
    ./{{OUTPUT_BIN}}

# Deploy to custom bin directory
deploy-custom: clean build
    mkdir -p ~/.custom/bin/
    cp ./{{OUTPUT_BIN}} ~/.custom/bin/

# Deploy to /usr/local/bin
deploy: clean build
    sudo cp ./{{OUTPUT_BIN}} /usr/local/bin/
    sudo chmod ug+x /usr/local/bin/{{NAME}}

# Clean build artifacts
clean:
    go clean
    rm -f {{OUTPUT_BIN}}
# Record the demo (scripts/demo/driver.py) with asciinema to bin/demo.cast; run it while a scrub is running
demo-record: build
    asciinema rec --headless --overwrite --window-size 179x42 --idle-time-limit 2 --quiet \
        --command "python3 scripts/demo/driver.py {{OUTPUT_BIN}} ." bin/demo.cast

# Render bin/demo.cast as screenshots/demo.gif with agg (https://github.com/asciinema/agg, a release binary on the PATH)
demo-gif:
    python3 scripts/demo/prepare_cast.py bin/demo.cast bin/demo-gif.cast
    {{AGG}} --font-size 14 --idle-time-limit 2 --fps-cap 15 bin/demo-gif.cast screenshots/demo.gif

# Record the demo and render it as screenshots/demo.gif
demo: demo-record demo-gif
