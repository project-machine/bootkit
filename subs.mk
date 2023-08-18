KEYSET ?= snakeoil
DOCKER_BASE ?= docker://
ifeq (${ARCH},aarch64)
UBUNTU_MIRROR := http://ports.ubuntu.com/ubuntu-ports
endif
UBUNTU_MIRROR ?= http://archive.ubuntu.com/ubuntu
KEYSET_D ?= $(HOME)/.local/share/machine/trust/keys/$(KEYSET)

STACKER_SUBS = \
	--substitute=KEYSET_D=$(KEYSET_D) \
	--substitute=DOCKER_BASE=$(DOCKER_BASE) \
	--substitute=UBUNTU_MIRROR=$(UBUNTU_MIRROR) \
	--substitute=HOME=$(HOME) \
	--substitute=KEYS_DIR=$(HOME)/.local/share/machine/trust/keys \
	--substitute=TOP_D=$(TOP_D)
