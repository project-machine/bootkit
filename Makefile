NAME := bootkit

include subs.mk
include common.mk

.PHONY: build
build:
	$(STACKER_RBUILD)

.PHONY: pkg-build
pkg-build:
	cd pkg && $(STACKER_BUILD)

LAYERS := $(shell cd $(TOP_D)/layers && \
				  for d in *; do [ -f "$$d/stacker.yaml" ] && echo "$$d"; done )

# probably too clever, but the foreach expands to layer-kernel layer-shim ...
# and then $@ replaces 'layer-' with 'layers/' resulting in
#   --stacker-file=layers/<layer>/stacker.yaml
# The result is you can 'make layer-shim' and also tab-complete that.
$(foreach d,$(LAYERS),layer-$(d)):
	$(STACKER_BUILD) "--stacker-file=$(subst layer-,layers/,$@)/stacker.yaml"

.PHONY: publish
publish:
	@$(call required_var,PUBLISH_USER)
	@$(call required_var,PUBLISH_PASSWORD)
	@$(call required_var,PUBLISH_URL)
	@echo publishing with $(PUBLISH_USER):SECRET to $(PUBLISH_URL)
	@$(STACKER_PUBLISH)

.PHONY: stacker-clean
stacker-clean:
	stacker $(STACKER_OPTS) clean
