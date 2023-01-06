NAME := bootkit

include subs.mk
include common.mk

.PHONY: build
build:
	$(STACKER_RBUILD)

.PHONY: pkg-build
pkg-build:
	cd pkg && $(STACKER_BUILD)
