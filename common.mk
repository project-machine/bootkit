TOP_D := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))

MAIN_VERSION = $(shell git describe --tags --always \
        "--match=[0-9].[0-9]*" "--match=[0-9][0-9].[0-9]*" \
        "--match=[0-9]-dev[0-9]*" "--match=[0-9][0-9]-dev[0-9]" \
        || echo no-git)
ifeq ($(MAIN_VERSION),$(filter $(MAIN_VERSION), "", no-git))
$(error "Bad value for MAIN_VERSION: '$(MAIN_VERSION)'")
endif

SERIAL = $(shell date --utc "+%y%m%d")
MAIN_SERIAL = $(MAIN_VERSION)+$(SERIAL)

BUILD_D ?= $(abspath $(TOP_D)/../build-$(NAME))
STACKER_D ?= $(BUILD_D)/stacker
ROOTS_D ?= $(BUILD_D)/roots
OCI_D ?= $(BUILD_D)/oci

# STACKER_COMMON_OPTS = --debug
# STACKER_BUILD_ARGS = --shell-fail
STACKER_OPTS = --stacker-dir=$(STACKER_D) --roots-dir=$(ROOTS_D) --oci-dir=$(OCI_D) $(STACKER_COMMON_OPTS)
STACKER_BUILD = stacker $(STACKER_OPTS) build $(STACKER_BUILD_ARGS) --layer-type=squashfs $(STACKER_SUBS)
STACKER_RBUILD = stacker $(STACKER_OPTS) recursive-build $(STACKER_BUILD_ARGS) --layer-type=squashfs $(STACKER_SUBS)

debug:
	@echo MAIN_VERSION=$(MAIN_VERSION)
	@echo MAIN_SERIAL=$(MAIN_SERIAL)
	@echo BUILD_D=$(BUILD_D)
	@echo SUBS=$(SUBS)
