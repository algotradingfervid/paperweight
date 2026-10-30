# Build commands used by every work package. See README.md.

GOLANGCI_LINT := bin/tools/golangci-lint

.PHONY: generate build test e2e lint checkids covertouched ci

# CD-45: go generate (CSS bundle and icons), then sqlc for every sqlc.yaml, then templ.
# Generated files are committed.
generate:
	go generate ./...
	@for f in $$(find internal -name sqlc.yaml 2>/dev/null | sort); do \
		echo "go tool sqlc generate -f $$f"; \
		go tool sqlc generate -f $$f || exit 1; \
	done
	go tool templ generate

build:
	CGO_ENABLED=0 go build -trimpath -o bin/app ./cmd/app

# The nestcheck tag turns on db's gate: a transaction opened on a goroutine
# already inside one prints the stack and exits the test binary with code 2.
test:
	go test -race -count=1 -tags nestcheck ./...

# CD-45: end-to-end tests.
e2e:
	go test -tags e2e -count=1 ./...

lint:
	$(GOLANGCI_LINT) run

# WP done-check (spec section 9 rule 3). Usage: make checkids AREA=<AREA>
checkids:
	@test -n "$(AREA)" || { echo "AREA is not set. Run: make checkids AREA=<AREA>"; exit 1; }
	go run ./scripts/checktestids $(AREA) --exclude-level e2e

# Touched-line coverage (CD-185). Usage: make covertouched BASE=<rev> PKGS=<pkgs>
# -coverpkg takes the PKGS patterns comma-separated, so each package's code
# counts as covered by any of the PKGS tests.
comma := ,
space := $(subst ,, )
covertouched:
	@test -n "$(BASE)" -a -n "$(PKGS)" || { echo "Run: make covertouched BASE=<rev> PKGS=./internal/<pkg>/..."; exit 1; }
	@mkdir -p .work
	go test -count=1 -coverpkg=$(subst $(space),$(comma),$(strip $(PKGS))) -coverprofile=.work/covertouched.out $(PKGS)
	go run ./scripts/covertouched -base $(BASE) -profile .work/covertouched.out

ci:
	$(MAKE) generate
	git diff --exit-code
	$(MAKE) lint test build
	go run ./scripts/checktestids --report
