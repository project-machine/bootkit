NAME := bootkit

include subs.mk
include common.mk

.PHONY: build
build:
	$(STACKER_RBUILD)
