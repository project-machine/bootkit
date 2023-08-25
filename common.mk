TOP_D := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))

MAIN_VERSION = $(shell git describe --tags --always \
        "--match=v[0-9].[0-9]*" "--match=v[0-9][0-9].[0-9]*" \
        || echo no-git)
ifeq ($(MAIN_VERSION),$(filter $(MAIN_VERSION), "", no-git))
$(error "Bad value for MAIN_VERSION: '$(MAIN_VERSION)'")
endif

SERIAL = $(shell date --utc "+%y%m%d")
MAIN_SERIAL = $(MAIN_VERSION).$(SERIAL)

BUILD_D ?= $(abspath $(TOP_D)/../build-$(NAME))
STACKER_D ?= $(BUILD_D)/stacker
ROOTS_D ?= $(BUILD_D)/roots
OCI_D ?= $(BUILD_D)/oci

STACKER_COMMON_OPTS = --debug
# STACKER_BUILD_ARGS = --shell-fail
STACKER_OPTS = --stacker-dir=$(STACKER_D) --roots-dir=$(ROOTS_D) --oci-dir=$(OCI_D) $(STACKER_COMMON_OPTS)
STACKER_BUILD = stacker $(STACKER_OPTS) build $(STACKER_BUILD_ARGS) --layer-type=squashfs --layer-type=tar $(STACKER_SUBS)
STACKER_RBUILD = stacker $(STACKER_OPTS) recursive-build $(STACKER_BUILD_ARGS) --search-dir=layers/ --layer-type=squashfs --layer-type=tar $(STACKER_SUBS)
STACKER_PUBLISH = stacker $(STACKER_OPTS) publish \
	--search-dir=layers/ --layer-type=squashfs $(STACKER_SUBS) \
	"--username=$(PUBLISH_USER)" "--password=$(PUBLISH_PASSWORD)" \
	"--url=$(PUBLISH_URL)" --tag=$(MAIN_SERIAL)

# use this as `$(call required_var,VARNAME)` to assert VARNAME is set.
# It expands to:
# [ -n "value-of-$VARNAME" ] || { echo "rule-name ...VARNAME"; exit 1; }
required_var = [ -n "$(value $1)" ] || { echo "$@ requires environment variable $1"; exit 1; }

ALL_GO_FILES := $(wildcard pkg/*.go pkg/*/*.go pkg/*/*/*.go)

pkg_build = vr() { echo "$$@" 1>&2; "$$@"; } ; \
		build() { for f in "$$@"; do \
		  vr go build -buildmode=exe -tags containers_image_openpgp "$$f" || break; done; } ; \
		  vr cd pkg && export GO_BIN=. && build $(1)
