KEYSET ?= snakeoil
DOCKER_BASE ?= docker://
UBUNTU_MIRROR ?= http://archive.ubuntu.com/ubuntu
HOME ?= \$HOME

STACKER_SUBS = \
	--substitute=KEYSET=$(KEYSET) \
	--substitute=DOCKER_BASE=$(DOCKER_BASE) \
	--substitute=UBUNTU_MIRROR=$(UBUNTU_MIRROR) \
	--substitute=HOME=$(HOME) \
	--substitute=TOP_D=$(TOP_D)
