NAME := bootkit

include subs.mk
include common.mk

.PHONY: build
build:
	$(STACKER_RBUILD)

.PHONY: pkg-build
pkg-build:
	cd pkg && $(STACKER_BUILD)

.PHONY: publish
publish:
	@$(call required_var,PUBLISH_USER)
	@$(call required_var,PUBLISH_PASSWORD)
	@$(call required_var,PUBLISH_URL)
	@echo publishing with $(PUBLISH_USER):SECRET to $(PUBLISH_URL)
	@$(STACKER_PUBLISH)
