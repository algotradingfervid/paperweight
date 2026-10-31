# Build commands used by every work package. See README.md.

GOLANGCI_LINT := bin/tools/golangci-lint

.PHONY: generate build test e2e lint checkids covertouched mutate ci handin e2e-quick briefcds

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
# The tests on the second line nest on purpose, so they run without the gate.
test:
	go test -race -count=1 -timeout 40m -tags nestcheck ./...
	go test -race -count=1 -run 'TestCanTx_Reads|TestNoSecondConn_(NestedReadFails|IgnoresCtxStillFails)' ./internal/kernel/auth ./internal/testkit

# CD-45: end-to-end tests.
e2e:
	go test -tags e2e -count=1 -timeout 30m ./...

lint:
	$(GOLANGCI_LINT) run --allow-serial-runners
	$(GOLANGCI_LINT) run --allow-serial-runners --build-tags nestcheck ./internal/platform/db/...

# WP done-check (spec section 9 rule 3). Usage: make checkids AREA=<AREA>
checkids:
	@test -n "$(AREA)" || { echo "AREA is not set. Run: make checkids AREA=<AREA>"; exit 1; }
	go run ./scripts/checktestids $(AREA) --exclude-level e2e

# Touched-line coverage (CD-185). Usage: make covertouched BASE=<rev> PKGS=<pkgs> [PROFILE=<file>]
# -coverpkg takes the PKGS patterns comma-separated, so each package's code
# counts as covered by any of the PKGS tests. The profile goes to PROFILE, or
# to a new temp file per run, so parallel runs never share one.
comma := ,
space := $(subst ,, )
covertouched:
	@test -n "$(BASE)" -a -n "$(PKGS)" || { echo "Run: make covertouched BASE=<rev> PKGS=./internal/<pkg>/..."; exit 1; }
	@p="$(PROFILE)"; [ -n "$$p" ] || p=$$(mktemp "$${TMPDIR:-/tmp}/covertouched.XXXXXX") || exit 1; \
	echo "go test -count=1 -coverpkg=$(subst $(space),$(comma),$(strip $(PKGS))) -coverprofile=$$p $(PKGS)"; \
	go test -count=1 -coverpkg=$(subst $(space),$(comma),$(strip $(PKGS))) -coverprofile="$$p" $(PKGS) || exit 1; \
	echo "go run ./scripts/covertouched -base $(BASE) -profile $$p"; \
	go run ./scripts/covertouched -base $(BASE) -profile "$$p"

# Mutation pass over touched lines and queries (CD-299). Usage: make mutate BASE=<rev> PKGS=<pkgs> [RUN=<regex>] [TAGS=<tags>]
mutate:
	@test -n "$(BASE)" -a -n "$(PKGS)" || { echo "Run: make mutate BASE=<rev> PKGS=./internal/<pkg>/... [RUN=<regex>] [TAGS=<tags>]"; exit 1; }
	go run ./scripts/mutate -base '$(BASE)' -pkgs '$(PKGS)' $(if $(RUN),-run '$(RUN)') $(if $(TAGS),-tags '$(TAGS)')

ci:
	$(MAKE) generate
	git diff --exit-code
	@out=$$($$(go env GOROOT)/bin/gofmt -l $$(git ls-files '*.go' | grep -v '_templ.go$$')); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(MAKE) lint test build
	go run ./scripts/checktestids --report

# Hand-in checks for one task (scripts/handin): gofmt, generated files up to
# date, tests (plus ./internal/ui/... for UI changes), the e2e route and sweep
# gates, checkids and lint. Every step runs; the exit code is 1 if any failed.
# Usage: make handin BASE=<rev> PKGS="<go package patterns>" [TAGS=<build tags>] [AREA=<test-id area>]
handin:
	@test -n "$(BASE)" -a -n "$(PKGS)" || { echo 'Run: make handin BASE=<rev> PKGS="./internal/<pkg>/..." [TAGS=<tags>] [AREA=<AREA>]'; exit 1; }
	go run ./scripts/handin -base '$(BASE)' -pkgs '$(PKGS)' -tags '$(TAGS)' -area '$(AREA)'

# Every package holding a _test.go file with an e2e build constraint.
E2E_PKGS = $(shell git grep --untracked -l -E '^//go:build.*e2e' -- '*_test.go' ':(exclude)*/testdata/*' | xargs -n1 dirname | sort -u | sed 's|^|./|')

# Some browser tests, across every package that holds them. Usage: make e2e-quick RUN=<regex>
# RUN is go test's -run regex. For a DocType, name its words as the tests do
# plus the shared document flows: RUN='Purchase|Doc_' for purchase.Order
# (TestE2E_Doc_PurchaseLifecycle, TestE2E_Doc_ButtonsPerState, ...),
# RUN='Expense|Attachment' for expense.Claim. After adding a GET route also run
# RUN='EveryRouteCovered|NotPagesAreChecked|EveryDocTypeSwept'. With no RUN it
# lists the tests in each package's e2e-tagged files.
e2e-quick:
	@if [ -z "$(RUN)" ]; then \
		echo "Usage: make e2e-quick RUN=<regex>   (go test -run; e.g. RUN='Purchase|Doc_')"; \
		echo "Tests in e2e-tagged files, by package:"; \
		for p in $(E2E_PKGS); do \
			echo "$$p"; \
			grep -l -E '^//go:build.*e2e' $$p/*_test.go | xargs grep -h -o -E '^func Test[A-Za-z0-9_]+' | sed 's/^func /  /' | sort; \
		done; \
		exit 1; \
	fi
	go test -tags e2e -count=1 -timeout 10m -run '$(RUN)' $(E2E_PKGS)

# CD entries new or changed between BASE and main that mention the given paths
# or packages (scripts/briefcds). Usage: make briefcds BASE=<rev> FILES="<paths or packages>"
briefcds:
	@test -n "$(BASE)" -a -n "$(FILES)" || { echo 'Run: make briefcds BASE=<rev> FILES="<paths or packages>"'; exit 1; }
	go run ./scripts/briefcds -base '$(BASE)' $(FILES)
