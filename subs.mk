KEYSET ?= snakeoil
DOCKER_BASE ?= docker://
UBUNTU_MIRROR ?= http://archive.ubuntu.com/ubuntu
KEYSET_D ?= $(HOME)/.local/share/machine/trust/keys/$(KEYSET)
MOSCTL_BINARY ?= https://github.com/project-machine/mos/releases/download/v0.0.14/mosctl
ZOT_BINARY ?= https://github.com/project-zot/zot/releases/download/v2.0.0-rc5/zot-linux-amd64-minimal

STACKER_SUBS = \
	--substitute=KEYSET_D=$(KEYSET_D) \
	--substitute=DOCKER_BASE=$(DOCKER_BASE) \
	--substitute=UBUNTU_MIRROR=$(UBUNTU_MIRROR) \
	--substitute=HOME=$(HOME) \
	--substitute=TOP_D=$(TOP_D) \
	--substitute=MOSCTL_BINARY=$(MOSCTL_BINARY) \
	--substitute=ZOT_BINARY=$(ZOT_BINARY)
